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
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Charon continuously monitors and cleans up orphaned worker processes
// Uses PID tracking instead of environment variable inspection for reliability
// Also detects and kills "stuck" workers that have exceeded timeout
type Charon struct {
	beadsClient           *beads.Client
	workerTracker         *WorkerTracker
	workerTimeout         time.Duration // How long before considering a worker stuck
	checkInterval         time.Duration
	orphanBeadGrace       time.Duration // How long an in_progress bead can live without a worker before recovery
	stopChan              chan struct{}
	stats                 JanitorStats
	getProjects           func() []types.Project
	activeWorkersSnapshot func() map[string]int
	onStaleActiveWorker   func(beadID string)
	lastWorktreeGC        time.Time // throttles the stale-worktree sweep
	registryStore         *registry.Store
	heartbeatHungAfter    time.Duration // stale registry heartbeat → hung worker
}

// JanitorStats tracks cleanup statistics
type JanitorStats struct {
	TotalScans            int
	OrphansFound          int
	StuckFound            int
	ProcessesKilled       int
	ProcessGroupsCleaned  int
	OrphanedChildrenFound int
	OrphanInProgressBeads int
	OrphanBeadsRecovered  int
	LastScanTime          time.Time
	LastOrphanTime        time.Time
	LastStuckTime         time.Time
	LastOrphanBeadTime    time.Time
}

// NewCharon creates a new orphan process janitor with timeout for stuck worker detection
func NewCharon(beadsClient *beads.Client, workerTracker *WorkerTracker, workerTimeout time.Duration) *Charon {
	return &Charon{
		beadsClient:        beadsClient,
		workerTracker:      workerTracker,
		workerTimeout:      workerTimeout,
		checkInterval:      60 * time.Second, // Check every minute
		orphanBeadGrace:    5 * time.Minute,  // Recover in_progress beads with no live worker after this long
		heartbeatHungAfter: 5 * time.Minute,  // Stale registry heartbeat → hung worker
		stopChan:           make(chan struct{}),
		stats:              JanitorStats{},
	}
}

// SetRegistry injects the worker registry for liveness-based stuck detection.
// Nil is valid: Charon then falls back to the legacy wall-clock timeout.
func (j *Charon) SetRegistry(st *registry.Store) {
	j.registryStore = st
}

// SetProjectsProvider lets the caller inject a function that returns the current
// list of projects so orphan recovery can iterate over every configured project
// instead of being limited to a single working directory.
func (j *Charon) SetProjectsProvider(fn func() []types.Project) {
	j.getProjects = fn
}

// SetActiveWorkersSnapshot lets the caller inject a function that returns a
// snapshot of the overseer's in-memory activeWorkers map (keyed by bead ID).
// Charon uses this to detect and clean up stale placeholder entries — beads
// whose worker process never recorded a real PID (PID == 0). The persistent
// worker tracker alone cannot detect this because the entry's status is
// still "running".
func (j *Charon) SetActiveWorkersSnapshot(fn func() map[string]int) {
	j.activeWorkersSnapshot = fn
}

// SetOnStaleActiveWorker registers a callback that Charon invokes when it
// removes a stale (PID 0) in-memory worker entry. The callback is responsible
// for actually deleting the entry from the overseer's activeWorkers map;
// Charon never touches that map directly because it is owned by the overseer.
func (j *Charon) SetOnStaleActiveWorker(fn func(beadID string)) {
	j.onStaleActiveWorker = fn
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

	// Sweep stale worktrees at most hourly: failed/abandoned chains leak one
	// worktree per bead (integration only removes them on clean completion).
	if time.Since(j.lastWorktreeGC) > time.Hour {
		j.lastWorktreeGC = time.Now()
		active := map[string]bool{}
		if j.activeWorkersSnapshot != nil {
			for beadID := range j.activeWorkersSnapshot() {
				active[beadID] = true
			}
		}
		go func() {
			if j.getProjects != nil {
				for _, project := range j.getProjects() {
					ReconcileOrphanedWorktrees(NewWorktreeManager(), project.Path, active)
				}
			}
			CleanupStaleWorktrees(24*time.Hour, active)
		}()
	}

	// Get tracked workers from PID database (full records with start times)
	trackedWorkers := j.workerTracker.GetRunningWorkerRecords()
	log.Printf("[Charon] 📊 Found %d tracked Hydra workers in database", len(trackedWorkers))

	// Sweep dead records FIRST. This must not depend on br: a worker that died
	// without cleanup leaves a phantom "running" record, and phantom records
	// make orphan-bead recovery believe a live worker exists. When this sweep
	// sat behind a failing getOpenBeads, phantoms were never cleared and
	// in_progress beads stayed stuck forever (fleet deadlock, 2026-07-31).
	liveWorkers := make(map[int]*WorkerRecord, len(trackedWorkers))
	for pid, record := range trackedWorkers {
		if !j.isProcessRunning(pid) {
			log.Printf("[Charon] ⚠️  Worker PID %d (bead %s) died without cleanup", pid, record.BeadID)
			j.workerTracker.MarkWorkerCompleted(pid, -1)
			continue
		}
		liveWorkers[pid] = record
	}

	// Recover orphan in_progress beads BEFORE the no-tracked-workers early return.
	// A worker crash mid-execution leaves beads in_progress with no live worker
	// and no tracked PID; the only way to unblock the swarm is to reopen them.
	recovered := j.recoverOrphanInProgressBeads()
	if recovered > 0 {
		log.Printf("[Charon] 🔧 Recovered %d orphan in_progress beads (reopened orphan bead for retry)", recovered)
	}

	if len(liveWorkers) == 0 {
		log.Printf("[Charon] ✅ No tracked workers to check")
		return
	}

	// Get open beads. Projects whose bead list fails are skipped rather than
	// aborting the whole scan; the orphan-kill decision below is fail-closed
	// per project, so a worker whose project could not be listed is never
	// killed (its bead is absent from the open map only due to the failure).
	openBeads, failedProjects := j.getOpenBeads()

	killCount := 0

	// Check each live tracked worker
	for pid, record := range liveWorkers {
		beadID := record.BeadID

		// Check if bead is still open
		if !openBeads[beadID] {
			if failedProjects[record.ProjectPath] {
				log.Printf("[Charon] ⚠️  Skipping orphan decision for PID %d (bead %s): bead list unavailable for project path %s", pid, beadID, record.ProjectPath)
				continue
			}
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

		// Check for stuck workers. With a registry, liveness is the
		// heartbeat: parked and actively-reporting workers are never killed
		// on wall time; only a stale heartbeat (hung) kills. Without a
		// registry row, the legacy wall-clock timeout applies.
		if kill, reason := j.shouldKillAsStuck(record, time.Now()); kill {
			runtime := time.Since(record.StartedAt)
			j.stats.StuckFound++
			j.stats.LastStuckTime = time.Now()
			log.Printf("[Charon] 😵 STUCK WORKER DETECTED: PID %d (bead %s) %s (running %v)",
				pid, beadID, reason, runtime)

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

	if killCount > 0 {
		log.Printf("[Charon] 🧹 Cleanup complete: killed %d processes (orphans + stuck)", killCount)
	} else {
		log.Printf("[Charon] ✅ No orphaned or stuck workers found")
	}

	// Clean up stale in-memory worker entries. The persistent worker tracker
	// already handles PID-based orphans, but a worker whose process dies before
	// its PID is recorded (so the tracker has PID 0) leaves a stuck "spawning"
	// entry in the overseer's activeWorkers map. That entry blocks all new
	// spawns for that bead with "worker already exists". We use the injected
	// snapshot to detect PID-0 entries and clear them.
	if cleared := j.clearStaleActiveWorkers(); cleared > 0 {
		log.Printf("[Charon] 🧹 Cleared %d stale in-memory worker entries (PID 0 placeholder)", cleared)
	}

	j.logStats()

	// Cleanup old records periodically (older than 24 hours)
	if err := j.workerTracker.CleanupOldWorkers(24 * time.Hour); err != nil {
		log.Printf("[Charon] Warning: failed to cleanup old records: %v", err)
	}
}

// getOpenBeads returns the set of open bead IDs across every configured
// project plus the set of project paths whose bead list could not be read.
// Iterating over projects (instead of just ".") lets Charon run from any
// working directory and still detect orphan beads across the full swarm. A
// failing project never aborts the scan: callers use the failed set to make
// fail-closed decisions for workers in those projects.
func (j *Charon) getOpenBeads() (map[string]bool, map[string]bool) {
	openBeads := make(map[string]bool)
	failedProjects := make(map[string]bool)

	projects := j.collectProjects()
	for _, project := range projects {
		beadList, err := j.beadsClient.ListBeads(project.Path, "")
		if err != nil {
			log.Printf("[Charon] Failed to list beads for project %s (path: %s): %v", project.Name, project.Path, err)
			failedProjects[project.Path] = true
			continue
		}

		for _, bead := range beadList {
			// Only consider beads that are not terminal (completed, closed, failed)
			if !bead.Status.IsTerminal() {
				openBeads[string(bead.ID)] = true
			}
		}
	}

	return openBeads, failedProjects
}

// shouldKillAsStuck decides whether a live tracked worker must be killed
// as stuck (PRD worker-park-and-resume Parts 4–5).
//
// With a registry row the decision is liveness-based: parked workers and
// workers with a fresh heartbeat are alive by definition — the reporter
// heartbeats every 30s even while the worker waits out provider congestion —
// so only a stale heartbeat means hung. Without a row (old worker,
// pre-registry daemon) the legacy wall-clock worker_timeout applies.
//
// This replaces the wall-clock sweep that on 2026-08-01 killed every worker
// at 15m: parked-on-403 workers, and one worker while it was emitting its
// closing verdict.
func (j *Charon) shouldKillAsStuck(record *WorkerRecord, now time.Time) (kill bool, reason string) {
	if j.registryStore != nil {
		row, ok, err := j.registryStore.GetWorker(record.BeadID)
		if err == nil && ok {
			age := now.Sub(row.LastHeartbeat)
			if age > j.heartbeatHungAfter {
				return true, fmt.Sprintf("registry heartbeat stale for %v (threshold %v), state=%s — hung", age, j.heartbeatHungAfter, row.State)
			}
			return false, fmt.Sprintf("registry heartbeat fresh (%v ago), state=%s", age, row.State)
		}
	}

	// Legacy: no registry data to judge liveness by.
	if j.workerTimeout > 0 && now.Sub(record.StartedAt) > j.workerTimeout {
		return true, fmt.Sprintf("no registry row and exceeds timeout %v", j.workerTimeout)
	}
	return false, ""
}

// markBeadFailed marks a bead as failed with a reason
func (j *Charon) markBeadFailed(beadID string, reason string) {
	// Recover the bead using CloseBead which retries with --force when the bead has
	// open children. This prevents a stuck worker from leaving a bead stuck in_progress.
	// We need to look up the project path from the worker tracker.
	log.Printf("[Charon] Marking bead %s as failed: %s", beadID, reason)

	// Find a worker record for this bead to get its project path.
	var projectPath string
	for _, rec := range j.workerTracker.GetRunningWorkerRecords() {
		if rec.BeadID == beadID {
			projectPath = rec.ProjectPath
			break
		}
	}
	if projectPath == "" {
		projectPath = "."
	}

	if err := j.beadsClient.CloseBead(projectPath, "", types.BeadID(beadID), reason); err != nil {
		log.Printf("[Charon] Failed to mark bead %s as failed: %v", beadID, err)
		return
	}
	log.Printf("[Charon] ✅ Marked bead %s as failed (close succeeded)", beadID)
}

// clearStaleActiveWorkers removes in-memory active worker entries that are stuck
// with a placeholder PID (0). These entries block all new spawns for the same
// bead with "worker already exists" because the persistent worker tracker
// shows them as status="running" even though no real process is alive.
//
// Returns the number of entries cleared.
func (j *Charon) clearStaleActiveWorkers() int {
	if j.activeWorkersSnapshot == nil {
		return 0
	}
	snapshot := j.activeWorkersSnapshot()
	if len(snapshot) == 0 {
		return 0
	}

	cleared := 0
	for beadID, pid := range snapshot {
		if pid != 0 {
			continue
		}
		// PID 0 with a real entry means the worker process died before its
		// connector recorded a real PID. Clear it from the persistent tracker
		// too so a future spawn isn't blocked by "worker already exists".
		if j.workerTracker != nil {
			if record, ok := j.workerTracker.GetWorkerByBead(beadID); ok && record.PID == 0 {
				if err := j.workerTracker.MarkWorkerCompleted(0, -1); err != nil {
					log.Printf("[Charon] Failed to mark stale worker %s completed: %v", beadID, err)
				}
			}
		}
		// The actual removal from o.activeWorkers happens via the Overseer's
		// removeActiveWorker, which Charon invokes via a callback.
		if j.onStaleActiveWorker != nil {
			j.onStaleActiveWorker(beadID)
		}
		cleared++
	}
	return cleared
}

// collectProjects returns the list of projects to scan for orphan beads. It prefers
// the injected provider (from the overseer) and falls back to scanning the current
// working directory for backwards compatibility.
func (j *Charon) collectProjects() []types.Project {
	if j.getProjects != nil {
		projects := j.getProjects()
		if len(projects) > 0 {
			return projects
		}
	}
	// Fallback: scan "." if no projects were injected.
	return []types.Project{{Name: "local", Path: "."}}
}

// recoverOrphanInProgressBeads force-closes beads that are stuck in_progress with no
// live worker. This happens when a worker crashes mid-execution (e.g. API timeout,
// OOM, network error) and the update-status path is the safe way to put it
// back on the scheduler: reopening the bead keeps dependents blocked until the
// work is actually redone, whereas closing would mark incomplete work as done.
//
// Without this recovery, a single stuck in_progress bead blocks all downstream beads
// whose DAG depends on it, indefinitely stalling the swarm.
func (j *Charon) recoverOrphanInProgressBeads() int {
	if j.orphanBeadGrace <= 0 {
		return 0
	}

	// Iterate over every configured project so we recover beads regardless of
	// hydra's working directory. Without this, running hydra in a directory
	// without a .beads/ folder would silently skip all projects.
	projects := j.collectProjects()
	grace := j.orphanBeadGrace

	liveWorkers := j.workerTracker.GetRunningWorkers()
	recovered := 0

	for _, project := range projects {
		beadList, err := j.beadsClient.ListBeads(project.Path, "")
		if err != nil {
			log.Printf("[Charon] ❌ Failed to list beads for orphan recovery in project %s (path: %s): %v", project.Name, project.Path, err)
			continue
		}

		for _, bead := range beadList {
			if bead.Status != types.StatusInProgress {
				continue
			}

			beadIDStr := string(bead.ID)

			// Check if any live tracked worker exists for this bead
			hasLive := false
			for _, trackedBeadID := range liveWorkers {
				if trackedBeadID == beadIDStr {
					hasLive = true
					break
				}
			}
			if hasLive {
				continue
			}

			j.stats.OrphanInProgressBeads++

			// Fail-closed on the destructive decision: without a usable timestamp we
			// cannot know whether the bead is older than the grace period, so we
			// must not race a worker that has spawned but not yet registered.
			if bead.UpdatedAt.IsZero() {
				log.Printf("[Charon] ⚠️  Skipping orphan recovery for bead %s: no UpdatedAt timestamp", beadIDStr)
				continue
			}
			if age := time.Since(bead.UpdatedAt); age < grace {
				log.Printf("[Charon] ⏳ Bead %s is only %v old, within %v grace period; leaving in_progress", beadIDStr, age, grace)
				continue
			}

			log.Printf("[Charon] 🔧 Recovering orphan in_progress bead %s (project: %s)", beadIDStr, project.Path)

			// Reopen the bead so the scheduler retries it. Never close here: closing
			// would mark incomplete work as done and falsely unblock dependents.
			if err := j.beadsClient.UpdateBeadStatus(project.Path, bead.ID, types.StatusOpen); err != nil {
				log.Printf("[Charon] ❌ Failed to reopen orphan bead %s: %v", beadIDStr, err)
				continue
			}

			j.stats.OrphanBeadsRecovered++
			j.stats.LastOrphanBeadTime = time.Now()
			recovered++
			log.Printf("[Charon] ✅ reopened orphan bead %s for retry", beadIDStr)
		}
	}

	return recovered
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

// isSystemProcess checks if a process is a system process that should not be killed
// Uses a whitelist of known system process names
func (j *Charon) isSystemProcess(comm string) bool {
	// Strip path prefix if present (e.g., /usr/sbin/sshd -> sshd)
	name := comm
	if idx := strings.LastIndex(comm, "/"); idx != -1 {
		name = comm[idx+1:]
	}

	// System process whitelist - these should never be killed
	systemProcesses := map[string]bool{
		"sshd":           true,
		"systemd":        true,
		"init":           true,
		"kernel_task":    true,
		"launchd":        true,
		"WindowServer":   true,
		"loginwindow":    true,
		"Dock":           true,
		"Finder":         true,
		"SystemUIServer": true,
		"kernel":         true,
	}

	return systemProcesses[name]
}
