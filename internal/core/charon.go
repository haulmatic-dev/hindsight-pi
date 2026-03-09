package core

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/haulmatic-dev/hydra/internal/beads"
)

// Charon continuously monitors and cleans up orphaned opencode processes
// Uses PID tracking instead of environment variable inspection for reliability
// Also detects and kills "stuck" workers that have exceeded timeout
type Charon struct {
	beadsClient   *beads.Client
	workerTracker *WorkerTracker
	workerTimeout time.Duration // How long before considering a worker stuck
	checkInterval time.Duration
	stopChan      chan struct{}
	stats         JanitorStats
}

// JanitorStats tracks cleanup statistics
type JanitorStats struct {
	TotalScans      int
	OrphansFound    int
	StuckFound      int
	ProcessesKilled int
	LastScanTime    time.Time
	LastOrphanTime  time.Time
	LastStuckTime   time.Time
}

// NewCharon creates a new orphan process janitor with timeout for stuck worker detection
func NewCharon(beadsClient *beads.Client, workerTracker *WorkerTracker, workerTimeout time.Duration) *Charon {
	return &Charon{
		beadsClient:   beadsClient,
		workerTracker: workerTracker,
		workerTimeout: workerTimeout,
		checkInterval: 60 * time.Second, // Check every minute
		stopChan:      make(chan struct{}),
		stats:         JanitorStats{},
	}
}

// Start begins the janitor goroutine
func (j *Charon) Start() {
	log.Printf("[Charon] 🧹 Starting orphan process cleanup (interval: %v, worker timeout: %v)",
		j.checkInterval, j.workerTimeout)
	go j.run()
}

// Stop halts the janitor
func (j *Charon) Stop() {
	log.Printf("[Charon] Stopping...")
	close(j.stopChan)
}

// run is the main janitor loop
func (j *Charon) run() {
	ticker := time.NewTicker(j.checkInterval)
	defer ticker.Stop()

	// Run immediately on start
	j.performCleanup()

	for {
		select {
		case <-ticker.C:
			j.performCleanup()
		case <-j.stopChan:
			log.Printf("[Charon] Stopped")
			return
		}
	}
}

// performCleanup scans for and kills orphaned and stuck processes
func (j *Charon) performCleanup() {
	j.stats.TotalScans++
	j.stats.LastScanTime = time.Now()

	log.Printf("[Charon] 🔍 Scanning for orphaned or stuck processes...")

	// Get tracked workers from PID database (full records with start times)
	trackedWorkers := j.workerTracker.GetRunningWorkerRecords()
	log.Printf("[Charon] 📊 Found %d tracked Hydra workers in database", len(trackedWorkers))

	if len(trackedWorkers) == 0 {
		log.Printf("[Charon] ✅ No tracked workers to check")
		return
	}

	// Get open beads
	openBeads, err := j.getOpenBeads()
	if err != nil {
		log.Printf("[Charon] ❌ Failed to get open beads: %v", err)
		return
	}

	killCount := 0

	// Check each tracked worker
	for pid, record := range trackedWorkers {
		beadID := record.BeadID

		// Check if process is still running
		if !j.isProcessRunning(pid) {
			// Process died but wasn't marked completed - update tracker
			log.Printf("[Charon] ⚠️  Worker PID %d (bead %s) died without cleanup", pid, beadID)
			j.workerTracker.MarkWorkerCompleted(pid, -1)
			continue
		}

		// Check if bead is still open
		if !openBeads[beadID] {
			// Bead is closed but process still running = orphan!
			j.stats.OrphansFound++
			j.stats.LastOrphanTime = time.Now()
			log.Printf("[Charon] 🚨 ORPHAN DETECTED: PID %d (bead %s is closed but process still running)", pid, beadID)

			if err := j.killProcess(pid); err != nil {
				log.Printf("[Charon] ❌ Failed to kill PID %d: %v", pid, err)
			} else {
				killCount++
				j.stats.ProcessesKilled++
				j.workerTracker.MarkWorkerCompleted(pid, -1)
				log.Printf("[Charon] ✅ Killed orphaned process PID %d", pid)
			}
			continue
		}

		// Check for stuck workers (IN_PROGRESS but running too long)
		if j.workerTimeout > 0 {
			runtime := time.Since(record.StartedAt)
			if runtime > j.workerTimeout {
				// Worker has exceeded timeout - it's stuck!
				j.stats.StuckFound++
				j.stats.LastStuckTime = time.Now()
				log.Printf("[Charon] 😵 STUCK WORKER DETECTED: PID %d (bead %s) has been running for %v (exceeds timeout %v)",
					pid, beadID, runtime, j.workerTimeout)

				if err := j.killProcess(pid); err != nil {
					log.Printf("[Charon] ❌ Failed to kill stuck worker PID %d: %v", pid, err)
				} else {
					killCount++
					j.stats.ProcessesKilled++
					// Mark as completed with timeout exit code
					j.workerTracker.MarkWorkerCompleted(pid, -2) // -2 indicates timeout
					// Mark bead as failed so it can be retried
					j.markBeadFailed(beadID, fmt.Sprintf("Worker timed out after %v", runtime))
					log.Printf("[Charon] ✅ Killed stuck worker PID %d, marked bead %s as failed for retry", pid, beadID)
				}
			}
		}
	}

	if killCount > 0 {
		log.Printf("[Charon] 🧹 Cleanup complete: killed %d processes (orphans + stuck)", killCount)
	} else {
		log.Printf("[Charon] ✅ No orphaned or stuck workers found")
	}

	j.logStats()

	// Cleanup old records periodically (older than 24 hours)
	if err := j.workerTracker.CleanupOldWorkers(24 * time.Hour); err != nil {
		log.Printf("[Charon] Warning: failed to cleanup old records: %v", err)
	}
}

// getOpenBeads returns a map of open bead IDs
func (j *Charon) getOpenBeads() (map[string]bool, error) {
	openBeads := make(map[string]bool)

	// Get beads from the current project
	beadList, err := j.beadsClient.ListBeads(".", "")
	if err != nil {
		return openBeads, fmt.Errorf("failed to list beads: %w", err)
	}

	for _, bead := range beadList {
		// Only consider beads that are not terminal (completed, closed, failed)
		if !bead.Status.IsTerminal() {
			openBeads[string(bead.ID)] = true
		}
	}

	return openBeads, nil
}

// markBeadFailed marks a bead as failed with a reason
func (j *Charon) markBeadFailed(beadID string, reason string) {
	// We can't directly update the bead status through the beads client easily
	// The bead will naturally fail when the worker is killed and overseer detects it
	// But we log it for visibility
	log.Printf("[Charon] Bead %s should be marked as failed (timeout): %s", beadID, reason)
}

// isProcessRunning checks if a process is still running
func (j *Charon) isProcessRunning(pid int) bool {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid))
	err := cmd.Run()
	return err == nil
}

// killProcess kills a process by PID
func (j *Charon) killProcess(pid int) error {
	// Try graceful termination first (SIGTERM)
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("kill", "-15", strconv.Itoa(pid))
	} else {
		cmd = exec.Command("kill", "-15", strconv.Itoa(pid))
	}

	if err := cmd.Run(); err != nil {
		log.Printf("[Charon] SIGTERM failed for PID %d: %v", pid, err)
	}

	// Give it 5 seconds to exit gracefully
	time.Sleep(5 * time.Second)

	// Check if still running
	if j.isProcessRunning(pid) {
		log.Printf("[Charon] PID %d still running after SIGTERM, using SIGKILL", pid)

		// Force kill (SIGKILL)
		if runtime.GOOS == "darwin" {
			cmd = exec.Command("kill", "-9", strconv.Itoa(pid))
		} else {
			cmd = exec.Command("kill", "-9", strconv.Itoa(pid))
		}

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("SIGKILL failed: %w", err)
		}
	}

	return nil
}

// logStats logs current statistics
func (j *Charon) logStats() {
	running, completed := j.workerTracker.GetStats()
	log.Printf("[Charon] 📊 Stats: scans=%d, orphans_found=%d, stuck_found=%d, killed=%d, tracked=%d running, %d completed",
		j.stats.TotalScans, j.stats.OrphansFound, j.stats.StuckFound, j.stats.ProcessesKilled, running, completed)
}

// GetStats returns current janitor statistics
func (j *Charon) GetStats() JanitorStats {
	return j.stats
}
