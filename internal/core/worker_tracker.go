package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WorkerRecord represents a tracked worker process
type WorkerRecord struct {
	PID         int       `json:"pid"`
	PGID        int       `json:"pgid"` // Process Group ID for cleanup
	BeadID      string    `json:"bead_id"`
	ProjectPath string    `json:"project_path"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Status      string    `json:"status"`
	ExitCode    int       `json:"exit_code,omitempty"`
	// SpawnHead is the git HEAD of the worktree at the moment this worker was
	// spawned. It is used by the session-resume pilot to detect when the
	// worktree has diverged from the state the resumed session claims.
	SpawnHead string `json:"spawn_head,omitempty"`
}

// WorkerTracker persists worker process information to JSON file
type WorkerTracker struct {
	mu       sync.RWMutex
	dataPath string
	workers  map[int]*WorkerRecord
}

// NewWorkerTracker creates a new worker tracker
func NewWorkerTracker(projectPath string) (*WorkerTracker, error) {
	dataPath := filepath.Join(projectPath, ".hydra", "workers.json")

	// Ensure directory exists
	dir := filepath.Dir(dataPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create hydra directory: %w", err)
	}

	wt := &WorkerTracker{
		dataPath: dataPath,
		workers:  make(map[int]*WorkerRecord),
	}

	// Load existing data
	if err := wt.load(); err != nil && !os.IsNotExist(err) {
		log.Printf("[WorkerTracker] Warning: failed to load existing data: %v", err)
	}

	return wt, nil
}

// load reads workers from disk
func (wt *WorkerTracker) load() error {
	data, err := os.ReadFile(wt.dataPath)
	if err != nil {
		return err
	}

	var records []*WorkerRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}

	wt.mu.Lock()
	defer wt.mu.Unlock()

	for _, r := range records {
		wt.workers[r.PID] = r
	}

	return nil
}

// save writes workers to disk
// Note: Caller must hold the appropriate lock
func (wt *WorkerTracker) save() error {
	records := make([]*WorkerRecord, 0, len(wt.workers))
	for _, r := range wt.workers {
		records = append(records, r)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(wt.dataPath, data, 0644)
}

// TrackWorker records a new worker process
func (wt *WorkerTracker) TrackWorker(pid int, beadID string, projectPath string) error {
	return wt.TrackWorkerWithPGID(pid, pid, beadID, projectPath) // Default PGID = PID
}

// TrackWorkerWithPGID records a new worker process with explicit PGID
func (wt *WorkerTracker) TrackWorkerWithPGID(pid int, pgid int, beadID string, projectPath string) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	wt.workers[pid] = &WorkerRecord{
		PID:         pid,
		PGID:        pgid,
		BeadID:      beadID,
		ProjectPath: projectPath,
		StartedAt:   time.Now(),
		Status:      "running",
	}

	if err := wt.save(); err != nil {
		return fmt.Errorf("failed to track worker %d: %w", pid, err)
	}

	log.Printf("[WorkerTracker] Tracked worker PID %d (PGID %d) for bead %s", pid, pgid, beadID)
	return nil
}

// UpdateWorkerPID updates a worker's PID from placeholder (0) to actual PID
// This is used when a worker is initially tracked with PID 0 and later gets a real PID
func (wt *WorkerTracker) UpdateWorkerPID(oldPID int, newPID int, beadID string) error {
	return wt.UpdateWorkerPIDWithPGID(oldPID, newPID, newPID, beadID) // Default PGID = newPID
}

// UpdateWorkerPIDWithPGID updates a worker's PID and PGID
func (wt *WorkerTracker) UpdateWorkerPIDWithPGID(oldPID int, newPID int, pgid int, beadID string) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	// Find and remove the old entry
	record, exists := wt.workers[oldPID]
	if !exists {
		return fmt.Errorf("worker with PID %d not found", oldPID)
	}

	// Verify this is the correct bead
	if record.BeadID != beadID {
		return fmt.Errorf("bead ID mismatch: expected %s, got %s", record.BeadID, beadID)
	}

	// Remove old entry and create new one with correct PID and PGID
	delete(wt.workers, oldPID)
	wt.workers[newPID] = &WorkerRecord{
		PID:         newPID,
		PGID:        pgid,
		BeadID:      record.BeadID,
		ProjectPath: record.ProjectPath,
		StartedAt:   record.StartedAt,
		Status:      record.Status,
	}

	if err := wt.save(); err != nil {
		return fmt.Errorf("failed to update worker PID from %d to %d: %w", oldPID, newPID, err)
	}

	log.Printf("[WorkerTracker] Updated worker PID from %d to %d (PGID %d) for bead %s", oldPID, newPID, pgid, record.BeadID)
	return nil
}

// MarkWorkerCompleted marks a worker as completed
func (wt *WorkerTracker) MarkWorkerCompleted(pid int, exitCode int) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	if worker, ok := wt.workers[pid]; ok {
		worker.Status = "completed"
		worker.CompletedAt = time.Now()
		worker.ExitCode = exitCode

		if err := wt.save(); err != nil {
			return fmt.Errorf("failed to mark worker %d completed: %w", pid, err)
		}

		log.Printf("[WorkerTracker] Marked worker PID %d as completed (exit: %d)", pid, exitCode)
	}

	return nil
}

// GetRunningWorkers returns all currently running worker PIDs
func (wt *WorkerTracker) GetRunningWorkers() map[int]string {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	workers := make(map[int]string)
	for pid, r := range wt.workers {
		if r.Status == "running" {
			workers[pid] = r.BeadID
		}
	}

	return workers
}

// GetRunningWorkerRecords returns full records for all running workers
func (wt *WorkerTracker) GetRunningWorkerRecords() map[int]*WorkerRecord {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	workers := make(map[int]*WorkerRecord)
	for pid, r := range wt.workers {
		if r.Status == "running" {
			// Return a copy to avoid race conditions
			workers[pid] = &WorkerRecord{
				PID:         r.PID,
				PGID:        r.PGID,
				BeadID:      r.BeadID,
				ProjectPath: r.ProjectPath,
				StartedAt:   r.StartedAt,
				Status:      r.Status,
			}
		}
	}

	return workers
}

// IsHydraWorker checks if a PID is a tracked Hydra worker
func (wt *WorkerTracker) IsHydraWorker(pid int) bool {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	_, ok := wt.workers[pid]
	return ok
}

// CleanupOldWorkers removes completed workers older than the specified duration
func (wt *WorkerTracker) CleanupOldWorkers(olderThan time.Duration) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for pid, r := range wt.workers {
		if r.Status == "completed" && r.CompletedAt.Before(cutoff) {
			delete(wt.workers, pid)
			removed++
		}
	}

	if removed > 0 {
		if err := wt.save(); err != nil {
			return fmt.Errorf("failed to cleanup old workers: %w", err)
		}
		log.Printf("[WorkerTracker] Cleaned up %d old worker records", removed)
	}

	return nil
}

// GetStats returns tracking statistics
func (wt *WorkerTracker) GetStats() (running, completed int) {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	for _, r := range wt.workers {
		if r.Status == "running" {
			running++
		} else {
			completed++
		}
	}

	return running, completed
}

// RemoveWorkerByBeadID deletes a tracked worker record by its bead ID.
// It is used to reap stale worker rows for beads that have already reached
// a terminal state, so they are not treated as live workers by orphan
// recovery or process cleanup.
func (wt *WorkerTracker) RemoveWorkerByBeadID(beadID string) (bool, error) {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	removed := false
	for pid, r := range wt.workers {
		if r.BeadID == beadID {
			delete(wt.workers, pid)
			removed = true
		}
	}
	if !removed {
		return false, nil
	}
	if err := wt.save(); err != nil {
		return false, fmt.Errorf("failed to remove worker %s: %w", beadID, err)
	}
	log.Printf("[WorkerTracker] Removed worker record for bead %s", beadID)
	return true, nil
}

// GetWorkerByBead finds a worker record by bead ID
func (wt *WorkerTracker) GetWorkerByBead(beadID string) (*WorkerRecord, bool) {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	for _, r := range wt.workers {
		if r.BeadID == beadID {
			// Return a copy
			return &WorkerRecord{
				PID:         r.PID,
				PGID:        r.PGID,
				BeadID:      r.BeadID,
				ProjectPath: r.ProjectPath,
				StartedAt:   r.StartedAt,
				Status:      r.Status,
				ExitCode:    r.ExitCode,
				CompletedAt: r.CompletedAt,
			}, true
		}
	}

	return nil, false
}

// GetWorkerByPGID finds a worker record by process group ID (PGID)
func (wt *WorkerTracker) GetWorkerByPGID(pgid int) (*WorkerRecord, bool) {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	for _, r := range wt.workers {
		if r.PGID == pgid && r.Status == "running" {
			// Return a copy
			return &WorkerRecord{
				PID:         r.PID,
				PGID:        r.PGID,
				BeadID:      r.BeadID,
				ProjectPath: r.ProjectPath,
				StartedAt:   r.StartedAt,
				Status:      r.Status,
				ExitCode:    r.ExitCode,
				CompletedAt: r.CompletedAt,
				SpawnHead:   r.SpawnHead,
			}, true
		}
	}

	return nil, false
}

// SetWorkerSpawnHead records the worktree HEAD for a tracked worker.
func (wt *WorkerTracker) SetWorkerSpawnHead(pid int, head string) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	r, ok := wt.workers[pid]
	if !ok {
		return fmt.Errorf("worker %d not found", pid)
	}
	r.SpawnHead = head
	return wt.save()
}

// GetPreviousWorkerRecordByBead returns the most recent record for a bead
// excluding the given PID. This is used to find the orphan attempt that
// preceded the current respawn.
func (wt *WorkerTracker) GetPreviousWorkerRecordByBead(beadID string, excludePID int) (*WorkerRecord, bool) {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	var best *WorkerRecord
	for _, r := range wt.workers {
		if r.BeadID != beadID || r.PID == excludePID {
			continue
		}
		if best == nil || r.StartedAt.After(best.StartedAt) {
			best = r
		}
	}
	if best == nil {
		return nil, false
	}
	copy := *best
	return &copy, true
}
