package core

import (
	"testing"
	"time"
)

// Sustained provider-capacity congestion (exit 7 — "concurrent request
// limit") was deliberately never fed to the breaker: a capacity blip clears
// in seconds and freezing the fleet over one is worse than retrying. But the
// blip assumption breaks when congestion lasts days — observed on
// srv-smart-engine bead co3u8 (2026-08-14→17): 219 rejected turns over 3
// days, each worker lifecycle re-parking for its 60-minute cap, dying exit 7,
// and being respawned to re-bill the whole transcript against a provider that
// was still at capacity. ~6.1M tokens for zero output.
//
// Rule: capacity failures are counted as a CONSECUTIVE streak. A streak that
// reaches the threshold while spanning at least capacityMinSpan is sustained
// congestion → pause spawning (escalating cooldown). A fast streak is a blip
// → no pause. Any successful worker completion resets the streak.

func TestProviderBreaker_CapacityBlipDoesNotPause(t *testing.T) {
	b := NewProviderBreaker("kimi-coding", "k3-256k", "", "")
	now := time.Now()
	b.now = func() time.Time { return now }

	// 6 capacity failures inside 10 minutes: a blip — no pause.
	for i := 0; i < capacityThreshold; i++ {
		b.RecordCapacityFailure()
		now = now.Add(2 * time.Minute)
	}
	if paused, _ := b.ShouldPause(); paused {
		t.Errorf("blip (6 failures in 10m) must not pause the fleet")
	}
}

func TestProviderBreaker_SustainedCapacityCongestionPauses(t *testing.T) {
	b := NewProviderBreaker("kimi-coding", "k3-256k", "", "")
	now := time.Now()
	b.now = func() time.Time { return now }

	// 6 capacity failures spanning 31 minutes: sustained — pause.
	for i := 0; i < capacityThreshold; i++ {
		now = now.Add(6*time.Minute + 12*time.Second) // ~6.2m apart → span ≈31m
		b.RecordCapacityFailure()
	}
	paused, remaining := b.ShouldPause()
	if !paused {
		t.Fatalf("sustained congestion (6 failures over 31m) must pause spawning")
	}
	if remaining < 14*time.Minute || remaining > 16*time.Minute {
		t.Errorf("first capacity cooldown = %v, want ~15m", remaining)
	}
}

func TestProviderBreaker_CapacityPauseEscalates(t *testing.T) {
	b := NewProviderBreaker("kimi-coding", "k3-256k", "", "")
	now := time.Now()
	b.now = func() time.Time { return now }

	trigger := func() {
		// Advance-then-record: the 6th failure is recorded at the measurement
		// instant, so remaining-cooldown assertions are exact.
		for i := 0; i < capacityThreshold; i++ {
			now = now.Add(6*time.Minute + 12*time.Second) // ~6.2m apart → span ≈31m
			b.RecordCapacityFailure()
		}
	}

	trigger() // → 15m
	trigger() // → 30m
	_, remaining := b.ShouldPause()
	if remaining < 29*time.Minute || remaining > 31*time.Minute {
		t.Errorf("second capacity cooldown = %v, want ~30m", remaining)
	}

	now = now.Add(31 * time.Minute) // let it expire
	trigger()                       // → 60m
	now = now.Add(61 * time.Minute) // let it expire
	trigger()                       // → 120m
	_, remaining = b.ShouldPause()
	if remaining < 119*time.Minute || remaining > 121*time.Minute {
		t.Errorf("fourth capacity cooldown = %v, want ~120m (cap)", remaining)
	}

	now = now.Add(121 * time.Minute)
	trigger() // beyond cap: stays 120m
	_, remaining = b.ShouldPause()
	if remaining < 119*time.Minute || remaining > 121*time.Minute {
		t.Errorf("fifth capacity cooldown = %v, want ~120m (capped)", remaining)
	}
}

func TestProviderBreaker_CapacitySuccessResetsStreak(t *testing.T) {
	b := NewProviderBreaker("kimi-coding", "k3-256k", "", "")
	now := time.Now()
	b.now = func() time.Time { return now }

	// 5 sustained-spaced failures, then a success, then 5 more: the streak
	// never reaches 6 consecutive, so no pause ever fires.
	for i := 0; i < capacityThreshold-1; i++ {
		b.RecordCapacityFailure()
		now = now.Add(31 * time.Minute)
	}
	b.RecordCapacitySuccess()
	for i := 0; i < capacityThreshold-1; i++ {
		b.RecordCapacityFailure()
		now = now.Add(31 * time.Minute)
	}
	if paused, _ := b.ShouldPause(); paused {
		t.Errorf("a success mid-streak must reset the consecutive count")
	}
}

func TestProviderBreaker_SparseCapacityFailuresDoNotPause(t *testing.T) {
	b := NewProviderBreaker("kimi-coding", "k3-256k", "", "")
	now := time.Now()
	b.now = func() time.Time { return now }

	// 5 failures spread over hours, each below threshold: no pause.
	for i := 0; i < capacityThreshold-1; i++ {
		b.RecordCapacityFailure()
		now = now.Add(45 * time.Minute)
	}
	if paused, _ := b.ShouldPause(); paused {
		t.Errorf("sparse failures (%d over hours) must not pause", capacityThreshold-1)
	}
}
