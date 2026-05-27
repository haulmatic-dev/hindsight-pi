package core

import (
	"sync"
	"time"
)

// StateController manages Overseer runtime state (running, paused, workers).
type StateController struct {
	mu                 sync.RWMutex
	running            bool
	paused             bool
	maxWorkers         int
	beadDiscoveryInterval time.Duration
}

// NewStateController creates a new state controller.
func NewStateController(maxWorkers int, beadDiscoveryInterval time.Duration) *StateController {
	return &StateController{
		maxWorkers:            maxWorkers,
		beadDiscoveryInterval: beadDiscoveryInterval,
	}
}

// IsRunning reports whether the overseer is running.
func (s *StateController) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// SetRunning sets the running state.
func (s *StateController) SetRunning(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = v
}

// IsPaused reports whether the overseer is paused.
func (s *StateController) IsPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paused
}

// Pause pauses the overseer.
func (s *StateController) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
}

// Resume resumes the overseer.
func (s *StateController) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = false
}

// SetMaxWorkers updates the max workers.
func (s *StateController) SetMaxWorkers(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if count < 0 {
		count = 0
	}
	s.maxWorkers = count
}

// GetMaxWorkers returns the max workers.
func (s *StateController) GetMaxWorkers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxWorkers
}

// SetBeadDiscoveryInterval updates the bead discovery interval.
func (s *StateController) SetBeadDiscoveryInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.beadDiscoveryInterval = d
}

// GetBeadDiscoveryInterval returns the bead discovery interval.
func (s *StateController) GetBeadDiscoveryInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.beadDiscoveryInterval
}
