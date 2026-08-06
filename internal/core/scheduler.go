package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Scheduler orchestrates the main scheduling loop.
type Scheduler struct {
	overseer *Overseer

	// congestionCounter returns the number of currently-parked workers; if nil
	// congestion pausing is disabled. Kept as a field so tests can inject a
	// fake counter without building a full overseer/registry.
	congestionCounter func() (int, error)

	// congestionPausedUntil is non-zero while a congestion pause is in effect.
	congestionPausedUntil time.Time
}

// NewScheduler creates a new scheduler.
func NewScheduler(overseer *Overseer) *Scheduler {
	s := &Scheduler{overseer: overseer}
	if overseer != nil && overseer.registryStore != nil {
		store := overseer.registryStore
		s.congestionCounter = func() (int, error) {
			recs, err := store.ListWorkers(registry.StateParked)
			if err != nil {
				return 0, err
			}
			cutoff := time.Now().UTC().Add(-congestionParkedMaxAge)
			n := 0
			for _, r := range recs {
				if r.LastHeartbeat.After(cutoff) {
					n++
				}
			}
			return n, nil
		}
	}
	return s
}

const (
	congestionPauseThreshold  = 3
	congestionPauseDuration   = 5 * time.Minute
	congestionParkedMaxAge    = 10 * time.Minute
	congestionEventLogThrottle = 5 * time.Minute
)

// shouldPauseForCongestion returns true when the fleet has enough live parked
// workers that spawning more would just pile onto a saturated provider.
// It is fail-open: no counter or counter error means do not pause.
func (s *Scheduler) shouldPauseForCongestion() (bool, error) {
	if s.congestionCounter == nil {
		return false, nil
	}

	now := time.Now().UTC()
	if !s.congestionPausedUntil.IsZero() && now.Before(s.congestionPausedUntil) {
		return true, nil
	}
	// Expired pause; clear it before re-evaluating so a stale pause cannot
	// outlive the recovery logic.
	s.congestionPausedUntil = time.Time{}

	parked, err := s.congestionCounter()
	if err != nil {
		log.Printf("[Scheduler] congestion counter failed (continuing): %v", err)
		return false, nil
	}
	if parked >= congestionPauseThreshold {
		s.congestionPausedUntil = now.Add(congestionPauseDuration)
		log.Printf("[Scheduler] 🚧 Congestion pause: %d live parked workers (threshold %d). Spawns paused until %v",
			parked, congestionPauseThreshold, s.congestionPausedUntil)
		return true, nil
	}
	return false, nil
}

// effectiveMaxWorkers returns the configured maxWorkers capped by the
// concurrency governor when one is enabled and its limit is positive. Zero or
// negative maxWorkers means unlimited and is returned unchanged so callers can
// detect it.
func (s *Scheduler) effectiveMaxWorkers(maxWorkers int) int {
	if maxWorkers <= 0 {
		return 0
	}
	if s.overseer == nil || s.overseer.concurrencyGovernor == nil {
		return maxWorkers
	}
	if limit := s.overseer.concurrencyGovernor.Limit(); limit > 0 && limit < maxWorkers {
		return limit
	}
	return maxWorkers
}

// getRetryEligibleBeads returns beads that are eligible for retry.
func (s *Scheduler) getRetryEligibleBeads(readyBeads []types.Bead) []types.Bead {
	o := s.overseer
	retrySnapshot := o.retryManager.Snapshot()
	if len(retrySnapshot) == 0 {
		return readyBeads
	}

	readyMap := make(map[string]types.Bead, len(readyBeads))
	for _, bead := range readyBeads {
		readyMap[string(bead.ID)] = bead
	}

	// Retry state now survives a spawn, so a bead being worked on right now is
	// still in the snapshot. Queueing it would race a second worker against the
	// live one for the same bead — the spawner would refuse it, and that refusal
	// would be recorded as another failure.
	o.workerMutex.RLock()
	active := make(map[string]bool, len(o.activeWorkers))
	for id := range o.activeWorkers {
		active[id] = true
	}
	o.workerMutex.RUnlock()

	for beadID := range retrySnapshot {
		if _, exists := readyMap[beadID]; exists {
			continue
		}

		if active[beadID] {
			continue
		}

		if !o.retryManager.ShouldRetry(beadID, o.config.Overseer.DefaultRetries) {
			continue
		}

		count, _, _, _ := o.retryManager.RetryInfo(beadID)
		maxRetries := o.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}

		if bead, err := s.getBeadFromDAG(beadID); err == nil {
			readyBeads = append(readyBeads, bead)
			log.Printf("[Retry] Bead %s added to retry queue (attempt %d/%d)",
				beadID, count+1, maxRetries)
		} else {
			var notFound *BeadNotFoundError
			if errors.As(err, &notFound) {
				// The bead is no longer in the DAG (completed or removed
				// elsewhere). Leaving it in the retry map makes this lookup
				// fail again on every tick and floods the error log, so drop
				// it from retry tracking instead of re-logging forever.
				o.retryManager.ClearRetry(beadID)
				log.Printf("[Retry] Bead %s no longer in DAG; cleared from retry tracking", beadID)
			} else {
				log.Printf("[Retry] Could not fetch bead %s for retry: %v", beadID, err)
			}
		}
	}

	return readyBeads
}

// getBeadFromDAG retrieves a bead from the DAG by ID.
func (s *Scheduler) getBeadFromDAG(beadID string) (types.Bead, error) {
	o := s.overseer
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return types.Bead{}, fmt.Errorf("DAG not initialized")
	}

	node, exists := o.dag.GetNode(beadID)
	if !exists {
		return types.Bead{}, &BeadNotFoundError{BeadID: beadID, Where: "DAG"}
	}

	return node.Bead, nil
}

// Tick performs one iteration of the scheduling loop.
func (s *Scheduler) Tick() error {
	o := s.overseer

	// Check if paused
	if o.IsPaused() {
		return nil
	}

	// Give a lowered concurrency limit a way back up. A cut made during a provider
	// outage cannot be undone by outcome volume alone, because the cut is what
	// suppresses outcome volume — so the clock has to drive it.
	o.concurrencyGovernor.MaybeRecover()

	// Get active worker count
	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()

	maxWorkers := o.GetMaxWorkers()
	effectiveMax := s.effectiveMaxWorkers(maxWorkers)

	// Handle max_workers = 0 as "unlimited"
	var availableSlots int
	if maxWorkers == 0 {
		availableSlots = 20
		log.Printf("📊 Unlimited worker mode active (max_workers=0), allowing up to %d slots per tick", availableSlots)
	} else {
		availableSlots = effectiveMax - activeCount
	}

	if availableSlots <= 0 {
		log.Printf("📊 All worker slots filled (%d/%d), skipping bead discovery", activeCount, maxWorkers)
		return nil
	}

	// Provider cooldown: when the active provider's circuit is open and there is
	// no distinct fallback to use, spawning only produces rejected API calls.
	// Without this gate an exhausted quota kept the fleet spawning 2-3
	// workers/minute for hours (observed 2026-07-27).
	if o.providerBreaker != nil {
		if paused, remaining := o.providerBreaker.ShouldPause(); paused {
			log.Printf("⏸️  Provider cooldown active — skipping spawns for %s", remaining.Round(time.Second))
			return nil
		}
	}

	// Congestion pause: when the fleet is already backed up with live parked
	// workers, spawning more workers just creates a larger pile that competes
	// for the same provider slots. Pause for a cooldown instead.
	if paused, err := s.shouldPauseForCongestion(); err != nil {
		log.Printf("[Scheduler] congestion check error (continuing): %v", err)
	} else if paused {
		return nil
	}

	// Build/refresh DAG from all beads
	if err := o.buildDAG(); err != nil {
		o.errorLogger.GracefulDegradation("Overseer", "buildDAG", "using existing DAG", err)
	}

	// Register chains only ONCE at startup
	if o.chainCompletionDetector != nil && !o.chainsRegistered {
		o.chainRegistrar.RegisterFromProjects(o.projects)
		o.chainsRegistered = true
	}

	// Get ready beads
	readyBeads, err := o.getReadyBeads()
	if err != nil {
		return fmt.Errorf("scheduler failed: %w", err)
	}

	// Include failed beads that are eligible for retry
	retryBeads := o.getRetryEligibleBeads(readyBeads)

	if len(retryBeads) == 0 {
		return nil
	}

	// Count retry beads separately for logging
	originalCount := len(readyBeads)
	retryCount := len(retryBeads) - originalCount

	if retryCount > 0 {
		log.Printf("📊 Active workers: %d/%d, Ready beads: %d (original: %d, retries: %d)",
			activeCount, maxWorkers, len(retryBeads), originalCount, retryCount)
	} else {
		log.Printf("📊 Active workers: %d/%d, Ready beads from DAG: %d", activeCount, maxWorkers, len(retryBeads))
	}

	// Apply 70% batch spawning rule
	batchLimit := int(float64(availableSlots) * 0.7)
	if batchLimit < 1 {
		batchLimit = 1
	}
	log.Printf("📊 Batch spawning limit: %d workers (70%% of %d available slots)", batchLimit, availableSlots)

	// Register chains before spawning workers
	if o.chainCompletionDetector != nil && !o.chainsRegistered && o.dag != nil {
		o.chainRegistrar.RegisterFromProjects(o.projects)
		o.chainsRegistered = true
		log.Printf("[ChainCompletion] Registered chains before worker spawn")
	}

	// Spawn workers for ready beads
	spawnedCount := 0
	resourceBlocked := 0
	poolBlocked := 0
	for _, bead := range retryBeads {
		// CRITICAL: Recalculate available slots before each spawn
		o.workerMutex.RLock()
		currentActive := len(o.activeWorkers)
		o.workerMutex.RUnlock()

		currentAvailable := s.effectiveMaxWorkers(maxWorkers) - currentActive
		if maxWorkers == 0 {
			currentAvailable = 20
		}

		if currentAvailable <= 0 || spawnedCount >= batchLimit {
			if spawnedCount >= batchLimit {
				log.Printf("📊 Reached batch limit (%d workers), deferring remaining beads to next tick", batchLimit)
			} else if currentAvailable <= 0 {
				log.Printf("📊 No available slots (active: %d/%d), breaking spawn loop", currentActive, maxWorkers)
			}
			break
		}

		beadIDStr := string(bead.ID)

		// Check if this is a retry
		if count, _, _, exists := o.retryManager.RetryInfo(beadIDStr); exists {
			log.Printf("[Retry] Attempting retry %d/%d for bead %s",
				count+1, o.config.Overseer.DefaultRetries, bead.ID)
		}

		// Skip approval gates
		if bead.Type == "approval-gate" {
			log.Printf("⏸️  Skipping approval gate: %s", bead.ID)
			continue
		}

		// Skip approval beads that are in progress
		if bead.Type == "approval" {
			log.Printf("⏸️  Skipping approval bead (waiting for user approval): %s", bead.ID)
			continue
		}

		// Skip if project path is not found
		projectPath := o.beadAnalyzer.FindProjectPath(&bead)
		if projectPath == "" {
			log.Printf("⚠️  Skipping bead %s: project '%s' not configured or found", bead.ID, bead.Project)
			continue
		}

		// Check resource limits before spawning
		if o.resourceMonitor != nil && !o.resourceMonitor.CanSpawnWorker() {
			log.Printf("[ResourceMonitor] Blocking worker spawn for bead %s: resource limits exceeded", bead.ID)
			resourceBlocked++
			continue
		}

		// Shared provider budget (Shepherd B3). Acquired BEFORE spawning, because
		// a spawned worker immediately starts calling the provider — asking
		// afterwards would be asking permission for something already happening.
		var poolRelease func()
		if o.providerPool != nil {
			acquireCtx, cancel := context.WithCancel(context.Background())
			rel, err := o.providerPool.Acquire(acquireCtx, "worker", o.providerPool.WeightFor("worker"))
			cancel()
			if err != nil {
				// No slot free. This is not a bead failure — leave it for the next
				// tick rather than burning a retry or spawning a doomed worker.
				log.Printf("[ProviderPool] No slot for bead %s (%s): %v", bead.ID, o.providerPool.Describe(), err)
				poolBlocked++
				continue
			}
			poolRelease = rel
		}

		// Check rate limiting
		if !o.spawnRateLimiter.CanSpawn() {
			log.Printf("[RateLimit] Blocking spawn for bead %s: rate limit exceeded", bead.ID)
			if poolRelease != nil {
				poolRelease()
			}
			break
		}

		if err := o.spawnWorker(bead); err != nil {
			// The worker never started, so nothing will ever release its slot.
			if poolRelease != nil {
				poolRelease()
			}
			errMsg := err.Error()
			isResourceConstraint := strings.Contains(errMsg, "worker limit reached") ||
				strings.Contains(errMsg, "rate limit exceeded") ||
				strings.Contains(errMsg, "resource limits exceeded")

			if !isResourceConstraint {
				o.retryManager.RecordRetry(beadIDStr, err)
			}
			o.errorLogger.Error("Overseer", "spawnWorker", beadIDStr, err, "error")
			continue
		}

		// Record successful spawn for rate limiting
		o.spawnRateLimiter.RecordSpawn()

		// Hand the reservation to the worker: WorkerLifecycle.Monitor releases it
		// when the process exits, however it exits.
		o.holdProviderSlot(beadIDStr, poolRelease)

		// Retry state is deliberately NOT cleared here. Clearing on spawn reset the
		// count on every attempt, so it could never reach 2 and the maxRetries cap
		// never fired once: across 2026-07-31 every recorded failure was "attempt 1"
		// and zero beads were ever abandoned. HandleSuccess clears it on actual
		// completion, which is the only outcome that proves the bead is fine.
		availableSlots--
		spawnedCount++
	}

	if spawnedCount > 0 {
		log.Printf("📊 Spawned %d workers this tick (batch limit: %d)", spawnedCount, batchLimit)
	}
	o.emitSpawnBlocked(resourceBlocked)
	if poolBlocked > 0 {
		log.Printf("📊 %d spawn(s) waited on the provider pool this tick (%s)", poolBlocked, o.providerPool.Describe())
	}

	return nil
}

// emitSpawnBlocked surfaces resource-blocked spawns in the event stream. The
// 2026-07-30 hindsight-api leak blocked every spawn for 2.5 hours with no
// signal outside per-bead log spam — a stalled fleet must be visible in the
// stream the shepherd cron watches. Throttled to one event per 5 minutes.
func (o *Overseer) emitSpawnBlocked(blocked int) {
	if blocked == 0 || o.beadCloser == nil {
		return
	}
	now := time.Now()
	if !o.lastSpawnBlockedEvent.IsZero() && now.Sub(o.lastSpawnBlockedEvent) < 5*time.Minute {
		return
	}
	o.lastSpawnBlockedEvent = now
	o.beadCloser.events.Emit(Event{Type: "spawn_blocked", Reason: "resource_limits",
		Fields: map[string]any{"blocked_this_tick": blocked}})
}

// TriggerImmediateSchedule spawns workers for ready beads without waiting for next poll.
func (s *Scheduler) TriggerImmediateSchedule() {
	o := s.overseer
	if o.IsPaused() {
		log.Println("Immediate scheduling skipped: overseer is paused")
		return
	}
	if !o.spawnRateLimiter.CanSpawn() {
		log.Println("[RateLimit] Immediate scheduling blocked: rate limit exceeded")
		return
	}
	availableSlots := s.GetAvailableWorkerSlots()
	if availableSlots <= 0 {
		return
	}
	readyNodes := s.GetReadyNodes()
	if len(readyNodes) == 0 {
		return
	}
	log.Printf("Immediate schedule: %d ready beads available", len(readyNodes))
	spawned := s.SpawnWorkersForNodes(readyNodes, availableSlots)
	if spawned > 0 {
		log.Printf("Immediate scheduling complete: spawned %d workers", spawned)
	}
}

// GetAvailableWorkerSlots returns the number of available worker slots.
func (s *Scheduler) GetAvailableWorkerSlots() int {
	o := s.overseer
	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()
	maxWorkers := o.GetMaxWorkers()
	var available int
	if maxWorkers == 0 {
		available = 20 - activeCount
	} else {
		available = s.effectiveMaxWorkers(maxWorkers) - activeCount
	}
	if available <= 0 {
		log.Printf("No available slots (%d/%d workers active)", activeCount, maxWorkers)
	}
	return available
}

// GetReadyNodes returns nodes that are ready for execution.
func (s *Scheduler) GetReadyNodes() []*Node {
	o := s.overseer
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()
	return o.dag.GetReadyNodes()
}

// SpawnWorkersForNodes spawns workers for the given nodes up to available slots.
func (s *Scheduler) SpawnWorkersForNodes(nodes []*Node, maxSlots int) int {
	o := s.overseer
	spawned := 0
	for _, node := range nodes {
		o.workerMutex.RLock()
		currentActive := len(o.activeWorkers)
		o.workerMutex.RUnlock()
		maxWorkers := o.GetMaxWorkers()
		currentAvailable := s.effectiveMaxWorkers(maxWorkers) - currentActive
		if maxWorkers == 0 {
			currentAvailable = 20
		}
		effectiveLimit := maxSlots
		if currentAvailable < effectiveLimit {
			effectiveLimit = currentAvailable
		}
		if spawned >= effectiveLimit {
			if currentAvailable <= 0 {
				log.Printf("No available slots (active: %d/%d), stopping", currentActive, maxWorkers)
			}
			break
		}
		if !s.ShouldSpawnWorker(node) {
			continue
		}
		if err := o.spawnWorker(node.Bead); err != nil {
			log.Printf("Failed to spawn worker for bead %s: %v", node.Bead.ID, err)
			continue
		}
		spawned++
	}
	return spawned
}

// ShouldSpawnWorker determines if a worker should be spawned for a node.
func (s *Scheduler) ShouldSpawnWorker(node *Node) bool {
	o := s.overseer
	beadID := string(node.Bead.ID)
	if o.isBeadRunning(beadID) {
		return false
	}
	if node.Bead.Type == "approval-gate" {
		log.Printf("Skipping approval gate: %s", beadID)
		return false
	}
	if node.Bead.Type == "approval" {
		log.Printf("Skipping approval bead (waiting for user approval): %s", beadID)
		return false
	}
	if node.Bead.Type == "epic" {
		log.Printf("Skipping epic (organizational): %s", beadID)
		return false
	}
	return true
}
