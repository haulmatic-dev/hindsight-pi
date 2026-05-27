package core

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// beadRetryInfo tracks retry state for a single bead (matches Overseer's original struct)
type beadRetryInfo struct {
	count     int
	lastError time.Time
	lastErr   error
}

// RetryManager encapsulates bead retry and validation-retry tracking.
// Extracted from Overseer to reduce god-object size.
type RetryManager struct {
	retries             map[string]*beadRetryInfo
	retriesMu           sync.RWMutex
	validationRetries   map[string]int
	validationRetriesMu sync.RWMutex
	baseDelay           time.Duration
	maxDelay            time.Duration
}

// NewRetryManager creates a retry manager with the given delay bounds.
func NewRetryManager(baseDelay, maxDelay time.Duration) *RetryManager {
	return &RetryManager{
		retries:           make(map[string]*beadRetryInfo),
		validationRetries: make(map[string]int),
		baseDelay:         baseDelay,
		maxDelay:          maxDelay,
	}
}

// ShouldRetry reports whether a bead has not exceeded maxRetries and cooldown has passed.
func (rm *RetryManager) ShouldRetry(beadID string, maxRetries int) bool {
	rm.retriesMu.RLock()
	defer rm.retriesMu.RUnlock()

	info, exists := rm.retries[beadID]
	if !exists {
		return true
	}

	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	if info.count >= maxRetries {
		log.Printf("[Retry] Bead %s exhausted all %d retry attempts", beadID, maxRetries)
		return false
	}

	backoff := calculateBackoff(info.count, rm.baseDelay, rm.maxDelay)
	minInterval := defaultRetryCooldown + backoff
	elapsed := time.Since(info.lastError)

	if elapsed < minInterval {
		log.Printf("[Retry] Bead %s in cooldown period (%v remaining)", beadID, minInterval-elapsed)
		return false
	}

	return true
}

// RecordRetry increments the retry count and returns the next backoff delay.
func (rm *RetryManager) RecordRetry(beadID string, err error) time.Duration {
	rm.retriesMu.Lock()
	defer rm.retriesMu.Unlock()

	info, ok := rm.retries[beadID]
	if !ok {
		info = &beadRetryInfo{}
		rm.retries[beadID] = info
	}
	info.count++
	info.lastError = time.Now()
	info.lastErr = err

	backoff := calculateBackoff(info.count, rm.baseDelay, rm.maxDelay)
	log.Printf("[Retry] Bead %s retry attempt %d recorded: %v", beadID, info.count, err)
	return backoff
}

// ClearRetry removes a bead from retry tracking.
func (rm *RetryManager) ClearRetry(beadID string) {
	rm.retriesMu.Lock()
	defer rm.retriesMu.Unlock()
	if _, exists := rm.retries[beadID]; exists {
		delete(rm.retries, beadID)
		log.Printf("[Retry] Bead %s retry state cleared after successful execution", beadID)
	}
}

// GetRetryCount returns the current retry count for a bead.
func (rm *RetryManager) GetRetryCount(beadID string) int {
	rm.retriesMu.RLock()
	defer rm.retriesMu.RUnlock()
	if info, exists := rm.retries[beadID]; exists {
		return info.count
	}
	return 0
}

// RetryInfo returns the retry info for a bead (count, lastErr, lastError time).
func (rm *RetryManager) RetryInfo(beadID string) (count int, lastErr error, lastError time.Time, ok bool) {
	rm.retriesMu.RLock()
	defer rm.retriesMu.RUnlock()
	if info, exists := rm.retries[beadID]; exists {
		return info.count, info.lastErr, info.lastError, true
	}
	return 0, nil, time.Time{}, false
}

// GetValidationRetryCount returns the validation retry count.
func (rm *RetryManager) GetValidationRetryCount(beadID string) int {
	rm.validationRetriesMu.RLock()
	defer rm.validationRetriesMu.RUnlock()
	return rm.validationRetries[beadID]
}

// IncrementValidationRetry increments the validation retry count.
func (rm *RetryManager) IncrementValidationRetry(beadID string) {
	rm.validationRetriesMu.Lock()
	defer rm.validationRetriesMu.Unlock()
	rm.validationRetries[beadID]++
	log.Printf("[Validation] Bead %s validation retry count: %d", beadID, rm.validationRetries[beadID])
}

// ClearValidationRetry removes a bead from validation retry tracking.
func (rm *RetryManager) ClearValidationRetry(beadID string) {
	rm.validationRetriesMu.Lock()
	defer rm.validationRetriesMu.Unlock()
	if _, exists := rm.validationRetries[beadID]; exists {
		delete(rm.validationRetries, beadID)
		log.Printf("[Validation] Bead %s validation retry state cleared", beadID)
	}
}

// String returns a debug summary.
func (rm *RetryManager) String() string {
	rm.retriesMu.RLock()
	rm.validationRetriesMu.RLock()
	defer rm.retriesMu.RUnlock()
	defer rm.validationRetriesMu.RUnlock()
	return fmt.Sprintf("RetryManager{retries:%d, validationRetries:%d}", len(rm.retries), len(rm.validationRetries))
}

// SetBaseDelay updates the base retry delay.
func (rm *RetryManager) SetBaseDelay(d time.Duration) {
	rm.baseDelay = d
}

// BaseDelay returns the current base retry delay.
func (rm *RetryManager) BaseDelay() time.Duration {
	return rm.baseDelay
}

// SetMaxDelay updates the maximum retry delay.
func (rm *RetryManager) SetMaxDelay(d time.Duration) {
	rm.maxDelay = d
}

// MaxDelay returns the current maximum retry delay.
func (rm *RetryManager) MaxDelay() time.Duration {
	return rm.maxDelay
}

// Snapshot returns a copy of all retry counts.
func (rm *RetryManager) Snapshot() map[string]int {
	rm.retriesMu.RLock()
	defer rm.retriesMu.RUnlock()
	result := make(map[string]int, len(rm.retries))
	for k, v := range rm.retries {
		result[k] = v.count
	}
	return result
}
