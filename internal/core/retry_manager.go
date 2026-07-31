package core

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// beadRetryInfo tracks retry state for a single bead (matches Overseer's original struct)
type beadRetryInfo struct {
	// count is the number of genuine bead faults — the only failures that
	// consume the retry budget. A bead is abandoned when this reaches maxRetries.
	count int
	// infraCount is the number of failures caused by the provider or the host
	// (429, timeout, overload, quota). These say nothing about the bead, so they
	// pace retries but never exhaust them.
	infraCount int
	lastError  time.Time
	lastErr    error
	isInfra    bool // true when the last failure was an infrastructure problem
}

// maxInfraRetries bounds how many times a bead is retried through provider
// failures at the normal exponential pace before it drops to hourly checks.
// It is not a give-up threshold: an overloaded provider must not permanently
// fail work that was never broken. It exists so that a provider outage lasting
// days costs one attempt an hour instead of one every few minutes.
const maxInfraRetries = 20

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

	// Only genuine bead faults exhaust the budget. Counting a 429 or a provider
	// overload against the bead means a busy provider permanently fails work that
	// was never broken — on 2026-07-31, 30 of 39 worker failures were exit 7.
	if info.count >= maxRetries {
		log.Printf("[Retry] Bead %s exhausted all %d retry attempts (%d infra failures were not counted)",
			beadID, maxRetries, info.infraCount)
		return false
	}

	if info.infraCount >= maxInfraRetries {
		if time.Since(info.lastError) >= time.Hour {
			return true
		}
		log.Printf("[Retry] Bead %s infra-throttled after %d provider failures, %v until next hourly retry",
			beadID, info.infraCount, (time.Hour - time.Since(info.lastError)).Round(time.Second))
		return false
	}

	// Backoff grows with total attempts, so a provider outage costs one attempt
	// per backoff window rather than one per ten-second tick.
	backoff := calculateBackoff(info.count+info.infraCount, rm.baseDelay, rm.maxDelay)
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
	info.lastError = time.Now()
	info.lastErr = err
	info.isInfra = isInfrastructureError(err)
	if info.isInfra {
		info.infraCount++
	} else {
		info.count++
	}

	backoff := calculateBackoff(info.count+info.infraCount, rm.baseDelay, rm.maxDelay)
	log.Printf("[Retry] Bead %s failure recorded (faults=%d, infra=%d, isInfra=%v, next backoff %v): %v",
		beadID, info.count, info.infraCount, info.isInfra, backoff, err)
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

// GetInfraRetryCount returns how many times a bead failed for provider or host
// reasons. These do not consume the retry budget, so callers deciding whether a
// bead is making progress must look at this as well as GetRetryCount.
func (rm *RetryManager) GetInfraRetryCount(beadID string) int {
	rm.retriesMu.RLock()
	defer rm.retriesMu.RUnlock()
	if info, exists := rm.retries[beadID]; exists {
		return info.infraCount
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

// isInfrastructureError classifies an error as infrastructure-related when it
// matches known transient patterns (rate limits, timeouts, network issues,
// daemon locks, etc.).  Used by RecordRetry to set the isInfra flag so that
// ShouldRetry can grant hourly retries after normal exhaustion.
func isInfrastructureError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "infrastructure failure") ||
		strings.Contains(s, "rate_limit") ||
		strings.Contains(s, "token plan") ||
		strings.Contains(s, "429") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "deadline exceeded") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "i/o error") ||
		strings.Contains(s, "permission denied") ||
		strings.Contains(s, "exit status 8") ||
		strings.Contains(s, "bead get failed") ||
		strings.Contains(s, "no such host") ||
		strings.Contains(s, "tls handshake")
}
