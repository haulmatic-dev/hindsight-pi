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
	BeadID      string    `json:"bead_id"`
	ProjectPath string    `json:"project_path"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Status      string    `json:"status"`
	ExitCode    int       `json:"exit_code,omitempty"`
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
func (wt *WorkerTracker) save() error {
	wt.mu.RLock()
	records := make([]*WorkerRecord, 0, len(wt.workers))
	for _, r := range wt.workers {
		records = append(records, r)
	}
	wt.mu.RUnlock()

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(wt.dataPath, data, 0644)
}

// TrackWorker records a new worker process
func (wt *WorkerTracker) TrackWorker(pid int, beadID string, projectPath string) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	wt.workers[pid] = &WorkerRecord{
		PID:         pid,
		BeadID:      beadID,
		ProjectPath: projectPath,
		StartedAt:   time.Now(),
		Status:      "running",
	}

	if err := wt.save(); err != nil {
		return fmt.Errorf("failed to track worker %d: %w", pid, err)
	}

	log.Printf("[WorkerTracker] Tracked worker PID %d for bead %s", pid, beadID)
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
