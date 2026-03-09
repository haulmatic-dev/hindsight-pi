package core

import (
	"sync"
	"time"
)

// ActivityMonitor tracks activity and implements resettable timeout
type ActivityMonitor struct {
	lastActivity time.Time
	timeout      time.Duration
	mu           sync.RWMutex
}

// NewActivityMonitor creates a new activity monitor
func NewActivityMonitor(timeout time.Duration) *ActivityMonitor {
	return &ActivityMonitor{
		lastActivity: time.Now(),
		timeout:      timeout,
	}
}

// RecordActivity records that activity occurred
func (am *ActivityMonitor) RecordActivity() {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.lastActivity = time.Now()
}

// IsTimedOut checks if timeout has elapsed since last activity
func (am *ActivityMonitor) IsTimedOut() bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return time.Since(am.lastActivity) > am.timeout
}

// TimeSinceActivity returns time since last activity
func (am *ActivityMonitor) TimeSinceActivity() time.Duration {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return time.Since(am.lastActivity)
}

// GetLastActivity returns the last activity timestamp
func (am *ActivityMonitor) GetLastActivity() time.Time {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.lastActivity
}
