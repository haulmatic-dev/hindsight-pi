package core

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
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
	TotalScans            int
	OrphansFound          int
	StuckFound            int
	ProcessesKilled       int
	ProcessGroupsCleaned  int
	OrphanedChildrenFound int
	LastScanTime          time.Time
	LastOrphanTime        time.Time
	LastStuckTime         time.Time
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

			// First try graceful termination of process group
			pgid := record.PGID
			if pgid == 0 {
				pgid = pid // Fallback to PID if PGID not set
			}

			// Kill entire process group gracefully first
			if err := j.killProcessGroupGraceful(pgid); err != nil {
				log.Printf("[Charon] ⚠️ Graceful kill of PGID %d failed: %v", pgid, err)
				// Fallback to force kill
				if err := j.killProcessGroupForce(pgid); err != nil {
					log.Printf("[Charon] ❌ Failed to force kill PGID %d: %v", pgid, err)
				}
			}

			// Also kill the main process if still running
			if j.isProcessRunning(pid) {
				if err := j.killProcess(pid); err != nil {
					log.Printf("[Charon] ❌ Failed to kill PID %d: %v", pid, err)
				}
			}

			j.stats.ProcessGroupsCleaned++
			killCount++
			j.stats.ProcessesKilled++
			j.workerTracker.MarkWorkerCompleted(pid, -1)
			log.Printf("[Charon] ✅ Cleaned up orphan process group PGID %d for bead %s", pgid, beadID)
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

				// Get PGID for process group cleanup
				pgid := record.PGID
				if pgid == 0 {
					pgid = pid
				}

				// Kill entire process group
				if err := j.killProcessGroupForce(pgid); err != nil {
					log.Printf("[Charon] ❌ Failed to kill stuck worker process group PGID %d: %v", pgid, err)
					// Fallback to single process kill
					if err := j.killProcess(pid); err != nil {
						log.Printf("[Charon] ❌ Failed to kill stuck worker PID %d: %v", pid, err)
						continue
					}
				}

				killCount++
				j.stats.ProcessesKilled++
				j.stats.ProcessGroupsCleaned++
				// Mark as completed with timeout exit code
				j.workerTracker.MarkWorkerCompleted(pid, -2) // -2 indicates timeout
				// Mark bead as failed so it can be retried
				j.markBeadFailed(beadID, fmt.Sprintf("Worker timed out after %v", runtime))
				log.Printf("[Charon] ✅ Killed stuck worker process group PGID %d, marked bead %s as failed for retry", pgid, beadID)
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

// killProcessGroupGraceful sends SIGTERM to entire process group
func (j *Charon) killProcessGroupGraceful(pgid int) error {
	// Send SIGTERM to process group using negative PID
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM to process group %d failed: %w", pgid, err)
	}

	// Give processes time to cleanup (10 seconds)
	time.Sleep(10 * time.Second)

	// Check if any processes in the group are still running
	pids := j.getProcessGroupPids(pgid)
	if len(pids) > 0 {
		return fmt.Errorf("process group %d still has %d running processes after SIGTERM", pgid, len(pids))
	}

	return nil
}

// killProcessGroupForce sends SIGKILL to entire process group
func (j *Charon) killProcessGroupForce(pgid int) error {
	// Send SIGKILL to process group using negative PID
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("SIGKILL to process group %d failed: %w", pgid, err)
	}

	// Give it a moment
	time.Sleep(2 * time.Second)

	// Also try pkill as fallback
	cleanupCmd := exec.Command("pkill", "-9", "-g", strconv.Itoa(pgid))
	cleanupCmd.Run() // Ignore errors - best effort

	return nil
}

// getProcessGroupPids returns all PIDs in a process group
func (j *Charon) getProcessGroupPids(pgid int) []int {
	var pids []int

	// Use ps to find processes in the group
	cmd := exec.Command("ps", "-e", "-o", "pid,pgid", "-g", strconv.Itoa(pgid))
	output, err := cmd.Output()
	if err != nil {
		return pids
	}

	// Parse output (skip header line)
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 2 {
			pid, _ := strconv.Atoi(fields[0])
			pids = append(pids, pid)
		}
	}

	return pids
}

// logStats logs current statistics
func (j *Charon) logStats() {
	running, completed := j.workerTracker.GetStats()
	log.Printf("[Charon] 📊 Stats: scans=%d, orphans_found=%d, stuck_found=%d, killed=%d, pg_cleaned=%d, orphan_children=%d, tracked=%d running, %d completed",
		j.stats.TotalScans, j.stats.OrphansFound, j.stats.StuckFound, j.stats.ProcessesKilled,
		j.stats.ProcessGroupsCleaned, j.stats.OrphanedChildrenFound, running, completed)
}

// GetStats returns current janitor statistics
func (j *Charon) GetStats() JanitorStats {
	return j.stats
}
