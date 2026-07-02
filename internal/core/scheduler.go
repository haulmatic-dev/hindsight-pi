package core

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Scheduler orchestrates the main scheduling loop.
type Scheduler struct {
	overseer *Overseer
}

// NewScheduler creates a new scheduler.
func NewScheduler(overseer *Overseer) *Scheduler {
	return &Scheduler{overseer: overseer}
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

	for beadID := range retrySnapshot {
		if _, exists := readyMap[beadID]; exists {
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

	// Get active worker count
	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()

	maxWorkers := o.GetMaxWorkers()

	// Handle max_workers = 0 as "unlimited"
	var availableSlots int
	if maxWorkers == 0 {
		availableSlots = 20
		log.Printf("📊 Unlimited worker mode active (max_workers=0), allowing up to %d slots per tick", availableSlots)
	} else {
		availableSlots = maxWorkers - activeCount
	}

	if availableSlots <= 0 {
		log.Printf("📊 All worker slots filled (%d/%d), skipping bead discovery", activeCount, maxWorkers)
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
	for _, bead := range retryBeads {
		// CRITICAL: Recalculate available slots before each spawn
		o.workerMutex.RLock()
		currentActive := len(o.activeWorkers)
		o.workerMutex.RUnlock()

		currentAvailable := maxWorkers - currentActive
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
		if bead.Type == "approval" && bead.Status == types.StatusInProgress {
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
			continue
		}

		// Check rate limiting
		if !o.spawnRateLimiter.CanSpawn() {
			log.Printf("[RateLimit] Blocking spawn for bead %s: rate limit exceeded", bead.ID)
			break
		}

		if err := o.spawnWorker(bead); err != nil {
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

		// Clear retry state on successful spawn
		o.retryManager.ClearRetry(beadIDStr)
		availableSlots--
		spawnedCount++
	}

	if spawnedCount > 0 {
		log.Printf("📊 Spawned %d workers this tick (batch limit: %d)", spawnedCount, batchLimit)
	}

	return nil
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
	available := maxWorkers - activeCount
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
		currentAvailable := maxWorkers - currentActive
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
	if node.Bead.Type == "approval" && node.Bead.Status == types.StatusInProgress {
		log.Printf("Skipping approval bead (waiting for user approval): %s", beadID)
		return false
	}
	if node.Bead.Type == "epic" {
		log.Printf("Skipping epic (organizational): %s", beadID)
		return false
	}
	return true
}
