package core

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// WorkerLifecycle manages worker process lifecycle from spawn to completion.
type WorkerLifecycle struct {
	retryManager    *RetryManager
	errorLogger     *ErrorLogger
	workerTracker   *WorkerTracker
	settledStateSvc *SettledStateService
	dag             *DAG
	dagMu           *sync.RWMutex
	completed       map[string]bool
	removeWorker    func(workerID string)
	closeBead       func(worker *types.Worker, success bool) error
	triggerSchedule func()
	notifyMCP       func(worker *types.Worker, success bool, exitCode int, duration time.Duration)
	getMaxRetries   func() int
	gitAuth         *GitAuth
	userContexts    map[string]*UserContext
}

// NewWorkerLifecycle creates a new worker lifecycle manager.
func NewWorkerLifecycle(
	retryManager *RetryManager,
	errorLogger *ErrorLogger,
	workerTracker *WorkerTracker,
	settledStateSvc *SettledStateService,
	dag *DAG,
	dagMu *sync.RWMutex,
	completed map[string]bool,
	removeWorker func(workerID string),
	closeBead func(worker *types.Worker, success bool) error,
	triggerSchedule func(),
	notifyMCP func(worker *types.Worker, success bool, exitCode int, duration time.Duration),
	getMaxRetries func() int,
) *WorkerLifecycle {
	return &WorkerLifecycle{
		retryManager:    retryManager,
		errorLogger:     errorLogger,
		workerTracker:   workerTracker,
		settledStateSvc: settledStateSvc,
		dag:             dag,
		dagMu:           dagMu,
		completed:       completed,
		removeWorker:    removeWorker,
		closeBead:       closeBead,
		triggerSchedule: triggerSchedule,
		notifyMCP:       notifyMCP,
		getMaxRetries:   getMaxRetries,
	}
}

// SetGitAuth sets the git auth manager for per-user commits.
func (wl *WorkerLifecycle) SetGitAuth(auth *GitAuth) {
	wl.gitAuth = auth
}

// SetUserContexts sets the user context map for looking up per-user credentials.
func (wl *WorkerLifecycle) SetUserContexts(ctxs map[string]*UserContext) {
	wl.userContexts = ctxs
}

// Monitor watches a worker process until completion.
func (wl *WorkerLifecycle) Monitor(worker *types.Worker, cmd *exec.Cmd) {
	completion := wl.Wait(worker, cmd)
	wl.HandleCompletion(completion)
}

// Wait waits for a worker process to complete and returns completion details.
func (wl *WorkerLifecycle) Wait(worker *types.Worker, cmd *exec.Cmd) workerCompletion {
	log.Printf("[DEBUG] waitForWorker: waiting for worker %s (PID %d)", worker.ID, worker.PID)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var err error
	select {
	case err = <-done:
	case <-time.After(30 * time.Minute):
		log.Printf("[Overseer] Worker %s timed out, killing process", worker.ID)
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("[Overseer] Failed to kill timed out worker: %v", err)
		}
		err = fmt.Errorf("worker timeout")
	}

	exitCode := extractExitCode(err)
	success := exitCode == 0
	duration := time.Since(worker.StartTime)
	log.Printf("[DEBUG] waitForWorker: worker %s finished with exitCode=%d, success=%v", worker.ID, exitCode, success)

	var newlyReady []string
	if success {
		newlyReady = wl.HandleSuccess(worker, duration)
	} else {
		wl.HandleFailure(worker, exitCode, duration)
	}

	wl.removeWorker(worker.ID)

	if wl.workerTracker != nil {
		if err := wl.workerTracker.MarkWorkerCompleted(worker.PID, exitCode); err != nil {
			log.Printf("[Overseer] Warning: failed to mark worker PID %d completed: %v", worker.PID, err)
		}
	}

	closeErr := wl.closeBead(worker, success)

	return workerCompletion{
		worker:     worker,
		success:    success,
		exitCode:   exitCode,
		duration:   duration,
		newlyReady: newlyReady,
		closeError: closeErr,
	}
}

// HandleCompletion handles the final steps after worker completion.
func (wl *WorkerLifecycle) HandleCompletion(comp workerCompletion) {
	success := comp.success

	if comp.closeError != nil {
		wl.errorLogger.Error("beadsClient", "CloseBead", comp.worker.ID, comp.closeError, "warn")
		if isDependencyError(comp.closeError) {
			log.Printf("[Worker] Bead %s processed but not closed due to dependencies - will retry later", comp.worker.BeadID.String())
			success = false
		}
	} else if success {
		wl.completed[comp.worker.ID] = true
		log.Printf("[Worker] Bead %s marked as completed in session", comp.worker.ID)
	}

	wl.scheduleAfterCompletion(comp.worker, success, comp.newlyReady)

	if wl.notifyMCP != nil {
		wl.notifyMCP(comp.worker, success, comp.exitCode, comp.duration)
	}
}

func (wl *WorkerLifecycle) HandleSuccess(worker *types.Worker, duration time.Duration) []string {
	log.Printf("[Worker] Completed: %s (duration: %v)", worker.ID, duration)
	worker.Status = string(types.WorkerStatusCompleted)
	wl.retryManager.ClearRetry(worker.ID)

	wl.dagMu.Lock()
	defer wl.dagMu.Unlock()

	newlyReady, err := wl.dag.MarkCompleted(worker.ID)
	if err != nil {
		wl.errorLogger.Error("DAG", "MarkCompleted", worker.ID, err, "warn")
		return nil
	}

	if len(newlyReady) > 0 {
		log.Printf("[DAG] %d dependents now ready after %s completion", len(newlyReady), worker.ID)
	}

	if wl.settledStateSvc != nil {
		if err := wl.settledStateSvc.TrackBead(worker.BeadID); err != nil {
			log.Printf("[SettledState] Failed to track bead %s: %v", worker.BeadID, err)
		}
	}

	// Per-user git commit/push on success (uses current branch, does not assume "main")
	if worker.Username != "" && wl.gitAuth != nil && wl.userContexts != nil {
		if ctx := wl.userContexts[worker.Username]; ctx != nil {
			result := wl.gitAuth.CommitAndPush(ctx, worker.ProjectPath, "hydra: complete bead "+worker.BeadID.String())
			if result.Error != nil {
				log.Printf("[GitAuth] Failed to commit for %s: %v", worker.Username, result.Error)
			} else if result.Success {
				log.Printf("[GitAuth] Committed for %s: %s", worker.Username, result.Hash)
			}
		}
	}

	return newlyReady
}

func (wl *WorkerLifecycle) HandleFailure(worker *types.Worker, exitCode int, duration time.Duration) {
	workerErr := fmt.Errorf("worker exited with code %d", exitCode)
	wl.retryManager.RecordRetry(worker.ID, workerErr)

	retryCount := wl.retryManager.GetRetryCount(worker.ID)
	maxRetries := wl.getMaxRetries()

	if retryCount >= maxRetries {
		log.Printf("[Worker] Failed permanently: %s (exit code: %d, duration: %v, retries: %d/%d)",
			worker.ID, exitCode, duration, retryCount, maxRetries)
		worker.Status = string(types.WorkerStatusFailed)
	} else {
		backoff := calculateBackoff(retryCount, wl.retryManager.BaseDelay(), wl.retryManager.MaxDelay())
		log.Printf("[Worker] Failed but will retry: %s (exit code: %d, duration: %v, retry %d/%d, backoff: %v)",
			worker.ID, exitCode, duration, retryCount, maxRetries, backoff)
		worker.Status = string(types.WorkerStatusRetryPending)
	}

	wl.dagMu.Lock()
	defer wl.dagMu.Unlock()
	if err := wl.dag.MarkFailed(worker.ID); err != nil {
		wl.errorLogger.Error("DAG", "MarkFailed", worker.ID, err, "warn")
	}
}

func (wl *WorkerLifecycle) scheduleAfterCompletion(worker *types.Worker, success bool, newlyReady []string) {
	if success {
		if len(newlyReady) > 0 {
			log.Printf("[Scheduler] Triggering immediate scheduling for %d newly ready beads", len(newlyReady))
		} else {
			log.Printf("[Scheduler] Worker completed, triggering immediate scheduling cycle")
		}
		wl.triggerSchedule()
	} else if wl.retryManager.GetRetryCount(worker.ID) < wl.getMaxRetries() {
		log.Printf("[Scheduler] Triggering scheduling to attempt retry for %s", worker.ID)
		wl.triggerSchedule()
	}
}


func isDependencyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "unresolved dependencies") || strings.Contains(errStr, "dependencies")
}
