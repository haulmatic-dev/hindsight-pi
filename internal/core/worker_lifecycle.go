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
	// releaseProviderSlot returns this worker's slot to the shared provider pool
	// (Shepherd B3). Tied to completion rather than to spawn returning: the
	// worker holds the provider connection for its whole life, so freeing the
	// slot at spawn time would let the pool over-admit.
	releaseProviderSlot func(workerID string)
	// concurrencyGovernor adapts effective concurrency based on observed outcomes.
	concurrencyGovernor *ConcurrencyGovernor // may be nil
	// workerTimeout caps ACTIVE (non-parked) wall time; defaults to 30m.
	// Set from config.Overseer.WorkerTimeout at the construction site.
	workerTimeout time.Duration
	// parkedLookup reports whether the worker is currently parked (provider
	// congestion). Parked time never counts against workerTimeout — the
	// worker's internal maxTotalRuntime already excludes it, and a blind
	// wall clock killed healthy workers mid-turn after long parks
	// (hydra-code-9p7, 2026-08-07). Nil lookup counts all time.
	parkedLookup func(beadID string) bool
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
	// Deferred, not called at the end of the happy path: a panic or an early
	// return here would otherwise leak a provider slot permanently, and a leaked
	// slot is worse than a refused request — it shrinks the budget silently and
	// forever.
	if wl.releaseProviderSlot != nil {
		defer wl.releaseProviderSlot(worker.ID)
	}
	completion := wl.Wait(worker, cmd)
	wl.HandleCompletion(completion)
}

// Wait waits for a worker process to complete and returns completion details.
// The timeout clock counts only ACTIVE time: while the worker is parked
// (provider congestion) the budget does not advance.
func (wl *WorkerLifecycle) Wait(worker *types.Worker, cmd *exec.Cmd) workerCompletion {
	log.Printf("[DEBUG] waitForWorker: waiting for worker %s (PID %d)", worker.ID, worker.PID)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	timeout := wl.workerTimeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	var active time.Duration
	last := time.Now()
	// Tick granularity follows the budget: fine enough to honor small
	// timeouts (and tests), capped so production (30m) ticks cheaply.
	tick := timeout / 10
	if tick < 50*time.Millisecond {
		tick = 50 * time.Millisecond
	}
	if tick > 5*time.Second {
		tick = 5 * time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	var err error
waitLoop:
	for {
		select {
		case err = <-done:
			break waitLoop
		case now := <-ticker.C:
			if wl.parkedLookup == nil || !wl.parkedLookup(worker.ID) {
				active += now.Sub(last)
			}
			last = now
			if active > timeout {
				log.Printf("[Overseer] Worker %s timed out after %v active (parked time excluded), killing process", worker.ID, active.Round(time.Second))
				if kerr := cmd.Process.Kill(); kerr != nil {
					log.Printf("[Overseer] Failed to kill timed out worker: %v", kerr)
				}
				err = fmt.Errorf("worker timeout")
				break waitLoop
			}
		}
	}

	exitCode := extractExitCode(err)
	success := exitCode == 0
	duration := time.Since(worker.StartTime)
	log.Printf("[DEBUG] waitForWorker: worker %s finished with exitCode=%d, success=%v", worker.ID, exitCode, success)

	// Feed observed provider-shaped outcomes to the adaptive concurrency governor.
	// Code-level failures (exit 1, 5, ...) are intentionally not recorded because
	// they describe bead defects, not provider health.
	//
	// Exit 7 is excluded for the same reason it is kept away from the circuit
	// breaker: it is a capacity blip that clears in seconds, not a signal about
	// our concurrency. Counting it made the governor and the provider pool two
	// controllers fighting over one actuator — on 2026-07-31 the pool was already
	// serializing the fleet to a single slot and the governor still walked the
	// worker limit 4 -> 2 -> 1 chasing a ceiling it could not see, so decreasing
	// could not possibly have helped. Sustained pressure still reaches the
	// governor through exit 2 (429) and exit 3 (timeout).
	if success {
		wl.concurrencyGovernor.RecordOutcome(true)
	} else if isConcurrencyPressureExitCode(exitCode) {
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
	// A completed worker proves the provider's concurrency ceiling has room
	// again — reset any sustained-capacity streak/escalation.
	if wl.providerBreaker != nil {
		wl.providerBreaker.RecordCapacitySuccess()
	}

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
	// Exit 7 (transient provider overload) does not open the rate-limit
	// circuit — blips clear in seconds. But a STREAK of exit 7 spanning
	// capacityMinSpan is sustained congestion (the co3u8 3-day storm), and
	// then spawning must pause: RecordCapacityFailure decides.
	if exitCode == 7 {
		if wl.providerBreaker != nil {
			wl.providerBreaker.RecordCapacityFailure()
		}
		log.Printf("[WorkerLifecycle] Transient provider overload (exit 7) for %s — retrying without tripping the rate-limit circuit", worker.ID)
	}
	wl.retryManager.RecordRetryWithCap(worker.ID, wl.getMaxRetries(), workerErr)

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

// isConcurrencyPressureExitCode reports whether a failure is evidence that we
// are asking the provider for too much at once, and therefore whether the
// adaptive concurrency governor should react to it.
//
// Only sustained pressure qualifies: 2 (429 rate limit) and 3 (timeout). Exit 7
// (transient overload) does not — see the reasoning at the call site.
func isConcurrencyPressureExitCode(code int) bool {
	return code == 2 || code == 3
}

// isInfrastructureExitCode returns true for exit codes that indicate transient
// infrastructure problems (rate limits, timeouts, network issues, daemon locks)
// rather than code-level problems.
func isInfrastructureExitCode(code int) bool {
	// Negative codes are signal deaths of the WORKER process itself (Charon's
	// hung-kill, OOM killer, operator kill). The worker's own code only ever
	// exits deliberately with 1/2/3/5/6/7, so a signal death is always an
	// external event — never a bead code fault.
	if code < 0 {
		return true
	}
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
