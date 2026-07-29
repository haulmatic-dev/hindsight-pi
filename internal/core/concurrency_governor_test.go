package core

import (
	"sync"
	"testing"
	"time"
)

func TestConcurrencyGovernor_FreshLimitEqualsMaxWorkers(t *testing.T) {
	g := NewConcurrencyGovernor(5, GovernorConfig{})
	if got := g.Limit(); got != 5 {
		t.Fatalf("fresh governor limit = %d, want 5", got)
	}
}

func TestConcurrencyGovernor_MultiplicativeDecrease(t *testing.T) {
	g := NewConcurrencyGovernor(5, GovernorConfig{})
	// 4 failures + 4 successes -> 0.5 failure fraction -> halve
	for i := 0; i < 4; i++ {
		g.RecordOutcome(false)
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 3 {
		t.Fatalf("after 50%% failures limit = %d, want 3", got)
	}
}

func TestConcurrencyGovernor_DecreaseFloorsAtOne(t *testing.T) {
	g := NewConcurrencyGovernor(2, GovernorConfig{})
	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}
	if got := g.Limit(); got != 1 {
		t.Fatalf("after repeated decreases limit = %d, want 1", got)
	}
}

func TestConcurrencyGovernor_DecreaseProgressionThreeToOne(t *testing.T) {
	g := NewConcurrencyGovernor(5, GovernorConfig{})
	// Drive limit 5 -> 3 with a 50% failure window.
	for i := 0; i < 4; i++ {
		g.RecordOutcome(false)
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 3 {
		t.Fatalf("setup: limit = %d, want 3", got)
	}
	// A further full window of failures drops 3 -> 2.
	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}
	if got := g.Limit(); got != 2 {
		t.Fatalf("after second decrease limit = %d, want 2", got)
	}
	// Another full window of failures drops 2 -> 1.
	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}
	if got := g.Limit(); got != 1 {
		t.Fatalf("after third decrease limit = %d, want 1", got)
	}
	// Further full windows must not drop below 1.
	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}
	if got := g.Limit(); got != 1 {
		t.Fatalf("limit dropped below 1: got %d, want 1", got)
	}
}

func TestConcurrencyGovernor_AdditiveIncrease(t *testing.T) {
	now := time.Now()
	g := NewConcurrencyGovernor(5, GovernorConfig{CooldownSeconds: 1})
	g.SetNow(func() time.Time { return now })

	// Drive limit down to 3.
	for i := 0; i < 4; i++ {
		g.RecordOutcome(false)
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 3 {
		t.Fatalf("setup: limit = %d, want 3", got)
	}

	// Move past cooldown and fill window with successes.
	g.SetNow(func() time.Time { return now.Add(2 * time.Second) })
	for i := 0; i < 8; i++ {
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 4 {
		t.Fatalf("after cooldown + successes limit = %d, want 4", got)
	}

	// Advance past the cooldown again so the next full window can rise once more.
	g.SetNow(func() time.Time { return now.Add(4 * time.Second) })

	// Another full window of successes raises to 5.
	for i := 0; i < 8; i++ {
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 5 {
		t.Fatalf("after second increase limit = %d, want 5", got)
	}
}

func TestConcurrencyGovernor_IncreaseRespectsCooldown(t *testing.T) {
	now := time.Now()
	g := NewConcurrencyGovernor(5, GovernorConfig{CooldownSeconds: 1})
	g.SetNow(func() time.Time { return now })

	// Drive limit down to 3 and immediately fill window with successes.
	for i := 0; i < 4; i++ {
		g.RecordOutcome(false)
		g.RecordOutcome(true)
	}
	for i := 0; i < 8; i++ {
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 3 {
		t.Fatalf("before cooldown elapsed limit should stay 3, got %d", got)
	}
}

func TestConcurrencyGovernor_DisabledWhenMaxWorkersZero(t *testing.T) {
	g := NewConcurrencyGovernor(0, GovernorConfig{})
	if got := g.Limit(); got != 0 {
		t.Fatalf("disabled governor limit = %d, want 0", got)
	}
	for i := 0; i < 100; i++ {
		g.RecordOutcome(false)
	}
	if got := g.Limit(); got != 0 {
		t.Fatalf("disabled governor limit after outcomes = %d, want 0", got)
	}
}

func TestConcurrencyGovernor_NilSafe(t *testing.T) {
	var g *ConcurrencyGovernor
	if got := g.Limit(); got != 0 {
		t.Fatalf("nil governor limit = %d, want 0", got)
	}
	g.RecordOutcome(true)  // must not panic
	g.RecordOutcome(false) // must not panic
	g.SetOnChange(nil)
	g.SetNow(time.Now)
}

func TestConcurrencyGovernor_OnChangeHook(t *testing.T) {
	g := NewConcurrencyGovernor(5, GovernorConfig{})
	var gotOld, gotNew int
	var gotReason string
	g.SetOnChange(func(old, new int, reason string) {
		gotOld, gotNew, gotReason = old, new, reason
	})
	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}
	if gotOld != 5 || gotNew != 3 || gotReason != "multiplicative_decrease" {
		t.Fatalf("onChange = (%d,%d,%q), want (5,3,multiplicative_decrease)", gotOld, gotNew, gotReason)
	}
}

func TestConcurrencyGovernor_ConcurrentRecordOutcome(t *testing.T) {
	g := NewConcurrencyGovernor(5, GovernorConfig{})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				g.RecordOutcome(j%2 == 0)
			}
		}()
	}
	wg.Wait()
	if limit := g.Limit(); limit < 1 || limit > 5 {
		t.Fatalf("limit %d out of allowed range [1,5]", limit)
	}
}
