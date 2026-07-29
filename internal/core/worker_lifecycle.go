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
	closeBead       func(worker *types.Worker, success bool, exitCode int) error
	triggerSchedule func()
	notifyMCP       func(worker *types.Worker, success bool, exitCode int, duration time.Duration)
	getMaxRetries   func() int
	providerBreaker *ProviderBreaker // rate-limit failover; may be nil
	// concurrencyGovernor adapts effective concurrency based on observed outcomes.
	concurrencyGovernor *ConcurrencyGovernor // may be nil
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
	closeBead func(worker *types.Worker, success bool, exitCode int) error,
	triggerSchedule func(),
	notifyMCP func(worker *types.Worker, success bool, exitCode int, duration time.Duration),
	getMaxRetries func() int,
	concurrencyGovernor *ConcurrencyGovernor,
) *WorkerLifecycle {
	return &WorkerLifecycle{
		retryManager:        retryManager,
		errorLogger:         errorLogger,
		workerTracker:       workerTracker,
		settledStateSvc:     settledStateSvc,
		dag:                 dag,
		dagMu:               dagMu,
		completed:           completed,
		removeWorker:        removeWorker,
		closeBead:           closeBead,
		triggerSchedule:     triggerSchedule,
		notifyMCP:           notifyMCP,
		getMaxRetries:       getMaxRetries,
		concurrencyGovernor: concurrencyGovernor,
	}
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

	// Feed observed provider-shaped outcomes to the adaptive concurrency governor.
	// Code-level failures (exit 1, 5, ...) are intentionally not recorded because
	// they describe bead defects, not provider health.
	if success {
		wl.concurrencyGovernor.RecordOutcome(true)
	} else if exitCode == 2 || exitCode == 3 || exitCode == 7 {
		wl.concurrencyGovernor.RecordOutcome(false)
	}

	var newlyReady []string
	if success {
		newlyReady = wl.HandleSuccess(worker, duration)
	} else {
		wl.HandleFailure(worker, exitCode, duration)
	}

	// Infrastructure-failure guard: do not close the bead (and therefore do not
	// create a fix bead) when the worker failed for a transient infra reason
	// and we still have retries left.  The bead stays in_progress in the beads
	// store; the scheduler will retry it after the cooldown.
	if !success && isInfrastructureExitCode(exitCode) {
		retryCount := wl.retryManager.GetRetryCount(worker.ID)
		maxRetries := wl.getMaxRetries()
		if retryCount < maxRetries {
			log.Printf("[WorkerLifecycle] Infra failure (exit %d) for %s, %d/%d retries left — leaving bead open for retry", exitCode, worker.ID, retryCount, maxRetries)
			wl.removeWorker(worker.ID)
			return workerCompletion{
				worker:   worker,
				success:  false,
				exitCode: exitCode,
				duration: duration,
			}
		}
		// Retries exhausted: fall through to closeBead.  BeadCloser will close
		// as failed with the infra-exhausted label and will NOT create a fix bead.
	}

	wl.removeWorker(worker.ID)

	if wl.workerTracker != nil {
		if err := wl.workerTracker.MarkWorkerCompleted(worker.PID, exitCode); err != nil {
			log.Printf("[Overseer] Warning: failed to mark worker PID %d completed: %v", worker.PID, err)
		}
	}

	closeErr := wl.closeBead(worker, success, exitCode)

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

	return newlyReady
}

func (wl *WorkerLifecycle) HandleFailure(worker *types.Worker, exitCode int, duration time.Duration) {
	var workerErr error
	if isInfrastructureExitCode(exitCode) {
		workerErr = fmt.Errorf("infrastructure failure (exit %d)", exitCode)
	} else {
		workerErr = fmt.Errorf("worker exited with code %d", exitCode)
	}
	// Exit code 2 == rate-limit/429 (classifyFailure). Feed the provider
	// circuit breaker so repeated 429s fail the next spawns over to the
	// fallback provider instead of hammering the throttled one.
	if exitCode == 6 && wl.providerBreaker != nil {
		// Quota exhausted: pause the whole fleet, do not try to fail over.
		wl.providerBreaker.RecordQuotaExhausted()
	}
	if exitCode == 2 && wl.providerBreaker != nil {
		wl.providerBreaker.RecordRateLimit()
	}
	// Exit 7 (transient provider overload) is deliberately absent from the
	// breaker path. It is retried like any infra failure, but it must not open a
	// circuit: three capacity blips inside ten minutes previously froze every
	// spawn for fifteen minutes over an error that clears in seconds.
	if exitCode == 7 {
		log.Printf("[WorkerLifecycle] Transient provider overload (exit 7) for %s — retrying without tripping the provider circuit", worker.ID)
	}
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

// isInfrastructureExitCode returns true for exit codes that indicate transient
// infrastructure problems (rate limits, timeouts, network issues, daemon locks)
// rather than code-level problems.
func isInfrastructureExitCode(code int) bool {
	// 6 = quota exhausted: infrastructure, never a code defect, so it must not
	// spawn a fix bead.
	// 7 = transient provider overload: same reasoning, and deliberately NOT fed
	// to the circuit breaker (see HandleFailure).
	return code == 2 || code == 3 || code == 4 || code == 6 || code == 7
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
