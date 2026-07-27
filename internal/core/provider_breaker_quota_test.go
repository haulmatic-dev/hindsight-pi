package core

import (
	"testing"
	"time"
)

// The deployed config sets fallback_provider equal to the primary, which made
// breaker failover a no-op by construction. ShouldPause is the gate that
// actually stops spawning in that situation.
func TestShouldPause_SelfReferentialFallback(t *testing.T) {
	b := NewProviderBreaker("kimi-coding", "kimi-for-coding", "kimi-coding", "kimi-for-coding")

	if paused, _ := b.ShouldPause(); paused {
		t.Fatal("must not pause before any failure")
	}

	b.RecordQuotaExhausted()

	paused, remaining := b.ShouldPause()
	if !paused {
		t.Fatal("quota exhaustion with no distinct fallback must pause spawning")
	}
	if remaining <= 0 || remaining > quotaCooldown {
		t.Errorf("remaining = %v, want between 0 and %v", remaining, quotaCooldown)
	}
}

// A real, distinct fallback means work can continue — no pause.
func TestShouldPause_DistinctFallbackKeepsRunning(t *testing.T) {
	b := NewProviderBreaker("primary", "m1", "other", "m2")
	b.RecordQuotaExhausted()
	if paused, _ := b.ShouldPause(); paused {
		t.Error("a distinct usable fallback should keep the fleet running")
	}
}

// Quota opens on the FIRST hit — unlike rate limits, it needs no threshold.
func TestRecordQuotaExhausted_OpensImmediately(t *testing.T) {
	b := NewProviderBreaker("p", "m", "p", "m")
	b.RecordQuotaExhausted()
	if paused, _ := b.ShouldPause(); !paused {
		t.Error("one quota rejection must be enough to pause")
	}
}

// A single rate-limit must NOT pause the fleet (threshold is 3, and its remedy
// is failover, not waiting).
func TestRecordRateLimit_DoesNotPauseOnFirstHit(t *testing.T) {
	b := NewProviderBreaker("p", "m", "other", "m2")
	b.RecordRateLimit()
	if paused, _ := b.ShouldPause(); paused {
		t.Error("a single 429 must not pause spawning")
	}
}

func TestQuotaCooldown_LongerThanRateLimitWindow(t *testing.T) {
	if quotaCooldown < 15*time.Minute {
		t.Errorf("quotaCooldown = %v; a billing limit clears on cycle rollover, so it must exceed the rate-limit openFor", quotaCooldown)
	}
}
