package core

import (
	"log"
	"sync"
	"time"
)

// SpawnRateLimiter tracks worker spawn timestamps and enforces rate limits
// and exponential backoff on consecutive failures.
type SpawnRateLimiter struct {
	spawnTimestamps []time.Time
	spawnRateMu     sync.Mutex

	lastSpawnFailure time.Time
	spawnFailures    int
	spawnBackoffMu   sync.Mutex
}

// NewSpawnRateLimiter creates a new spawn rate limiter.
func NewSpawnRateLimiter() *SpawnRateLimiter {
	return &SpawnRateLimiter{
		spawnTimestamps: make([]time.Time, 0),
	}
}

// CanSpawn reports whether a new worker can be spawned right now,
// considering rate limits and backoff.
func (s *SpawnRateLimiter) CanSpawn() bool {
	if !s.canSpawnWithinRateLimit() {
		return false
	}
	if s.shouldBackoff() {
		return false
	}
	return true
}

// canSpawnWithinRateLimit checks minimum interval since last spawn.
func (s *SpawnRateLimiter) canSpawnWithinRateLimit() bool {
	s.spawnRateMu.Lock()
	defer s.spawnRateMu.Unlock()

	now := time.Now()

	// Clean up old timestamps (keep last 5 minutes for metrics)
	cutoff := now.Add(-5 * time.Minute)
	valid := make([]time.Time, 0, len(s.spawnTimestamps))
	for _, ts := range s.spawnTimestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	s.spawnTimestamps = valid

	if len(s.spawnTimestamps) > 0 {
		last := s.spawnTimestamps[len(s.spawnTimestamps)-1]
		elapsed := now.Sub(last)
		if elapsed < minSpawnInterval {
			wait := minSpawnInterval - elapsed
			log.Printf("[RateLimit] Blocking spawn: last spawn %v ago, need to wait %v",
				elapsed.Round(time.Millisecond), wait.Round(time.Millisecond))
			return false
		}
	}
	return true
}

// RecordSpawn records a successful worker spawn timestamp.
func (s *SpawnRateLimiter) RecordSpawn() {
	s.spawnRateMu.Lock()
	defer s.spawnRateMu.Unlock()

	s.spawnTimestamps = append(s.spawnTimestamps, time.Now())
	log.Printf("[RateLimit] Spawn recorded: %d workers in last %v",
		len(s.spawnTimestamps), rateLimitWindow)
}

// RecordFailure records a spawn failure and returns the current backoff duration.
func (s *SpawnRateLimiter) RecordFailure() time.Duration {
	s.spawnBackoffMu.Lock()
	defer s.spawnBackoffMu.Unlock()

	s.spawnFailures++
	s.lastSpawnFailure = time.Now()

	backoff := time.Duration(1<<uint(s.spawnFailures-1)) * time.Second
	if backoff > 60*time.Second {
		backoff = 60 * time.Second
	}

	log.Printf("[SpawnRateLimiter] Failure #%d, backoff: %v", s.spawnFailures, backoff)
	return backoff
}

// ClearFailures resets the failure count after a successful spawn.
func (s *SpawnRateLimiter) ClearFailures() {
	s.spawnBackoffMu.Lock()
	defer s.spawnBackoffMu.Unlock()

	if s.spawnFailures > 0 {
		log.Printf("[SpawnRateLimiter] Failures cleared (was %d)", s.spawnFailures)
		s.spawnFailures = 0
	}
}

// shouldBackoff reports whether spawn backoff is currently active.
func (s *SpawnRateLimiter) shouldBackoff() bool {
	s.spawnBackoffMu.Lock()
	defer s.spawnBackoffMu.Unlock()

	if s.spawnFailures == 0 {
		return false
	}

	backoff := time.Duration(1<<uint(s.spawnFailures-1)) * time.Second
	if backoff > 60*time.Second {
		backoff = 60 * time.Second
	}

	elapsed := time.Since(s.lastSpawnFailure)
	shouldWait := elapsed < backoff

	if shouldWait {
		log.Printf("[SpawnRateLimiter] Backoff active: %v remaining", backoff-elapsed)
	}
	return shouldWait
}

// FailureCount returns the current consecutive failure count.
func (s *SpawnRateLimiter) FailureCount() int {
	s.spawnBackoffMu.Lock()
	defer s.spawnBackoffMu.Unlock()
	return s.spawnFailures
}

// SpawnCount returns the number of spawns in the last 5 minutes.
func (s *SpawnRateLimiter) SpawnCount() int {
	s.spawnRateMu.Lock()
	defer s.spawnRateMu.Unlock()
	return len(s.spawnTimestamps)
}
