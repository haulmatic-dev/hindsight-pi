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

func TestConcurrencyGovernor_AdditiveDecrease(t *testing.T) {
	g := NewConcurrencyGovernor(5, GovernorConfig{})
	// 4 failures + 4 successes -> 0.5 failure fraction -> step down one
	for i := 0; i < 4; i++ {
		g.RecordOutcome(false)
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 4 {
		t.Fatalf("after 50%% failures limit = %d, want 4", got)
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

// TestConcurrencyGovernor_DecreaseWalksDownOneStepPerWindow: the step size is
// the point. Halving dived from 4 straight to 1 on a single provider blip, and a
// limit of 1 is barely distinguishable from stopped.
func TestConcurrencyGovernor_DecreaseWalksDownOneStepPerWindow(t *testing.T) {
	g := NewConcurrencyGovernor(5, GovernorConfig{})
	for want := 4; want >= 1; want-- {
		for i := 0; i < 8; i++ {
			g.RecordOutcome(false)
		}
		if got := g.Limit(); got != want {
			t.Fatalf("limit = %d, want %d", got, want)
		}
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

	// Drive limit down to 4.
	for i := 0; i < 4; i++ {
		g.RecordOutcome(false)
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 4 {
		t.Fatalf("setup: limit = %d, want 4", got)
	}

	// Move past cooldown and fill window with successes.
	g.SetNow(func() time.Time { return now.Add(2 * time.Second) })
	for i := 0; i < 8; i++ {
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 5 {
		t.Fatalf("after cooldown + successes limit = %d, want 5", got)
	}
}

func TestConcurrencyGovernor_IncreaseRespectsCooldown(t *testing.T) {
	now := time.Now()
	g := NewConcurrencyGovernor(5, GovernorConfig{CooldownSeconds: 1})
	g.SetNow(func() time.Time { return now })

	// Drive limit down to 4 and immediately fill window with successes.
	for i := 0; i < 4; i++ {
		g.RecordOutcome(false)
		g.RecordOutcome(true)
	}
	for i := 0; i < 8; i++ {
		g.RecordOutcome(true)
	}
	if got := g.Limit(); got != 4 {
		t.Fatalf("before cooldown elapsed limit should stay 4, got %d", got)
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
	if g.MaybeRecover() {
		t.Fatal("nil governor reported a recovery")
	}
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
	if gotOld != 5 || gotNew != 4 || gotReason != "additive_decrease" {
		t.Fatalf("onChange = (%d,%d,%q), want (5,4,additive_decrease)", gotOld, gotNew, gotReason)
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

// TestConcurrencyGovernor_QuietRecoveryRaisesLimitWithoutOutcomes is the
// regression test for the recovery deadlock. On 2026-07-31 one provider capacity
// window took the limit to 1, and it stayed there for 2.5 hours on a single
// completion: a lower limit yields fewer outcomes, so the window never refilled,
// so nothing could ever raise it. Recovery must not depend on throughput.
func TestConcurrencyGovernor_QuietRecoveryRaisesLimitWithoutOutcomes(t *testing.T) {
	now := time.Now()
	g := NewConcurrencyGovernor(4, GovernorConfig{RecoveryIntervalSeconds: 300})
	g.SetNow(func() time.Time { return now })

	// A full window of failures drops the limit, exactly as the outage did.
	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}
	if got := g.Limit(); got != 3 {
		t.Fatalf("setup: limit = %d, want 3", got)
	}

	// Not yet quiet long enough.
	g.SetNow(func() time.Time { return now.Add(4 * time.Minute) })
	if g.MaybeRecover() {
		t.Error("recovered before the interval elapsed")
	}

	// Quiet for the full interval: climb by one, with no outcomes at all.
	g.SetNow(func() time.Time { return now.Add(6 * time.Minute) })
	if !g.MaybeRecover() {
		t.Fatal("no recovery after a full quiet interval — this is the deadlock")
	}
	if got := g.Limit(); got != 4 {
		t.Errorf("limit = %d after recovery, want 4", got)
	}
}

// TestConcurrencyGovernor_QuietRecoveryStopsAtMaxWorkers: recovery must never
// hand out more concurrency than the provider plan allows.
func TestConcurrencyGovernor_QuietRecoveryStopsAtMaxWorkers(t *testing.T) {
	now := time.Now()
	g := NewConcurrencyGovernor(3, GovernorConfig{RecoveryIntervalSeconds: 60})
	g.SetNow(func() time.Time { return now })

	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}
	for step := 0; step < 10; step++ {
		now = now.Add(2 * time.Minute)
		g.MaybeRecover()
	}
	if got := g.Limit(); got != 3 {
		t.Errorf("limit = %d after repeated recovery, want the max of 3", got)
	}
}

// TestConcurrencyGovernor_QuietRecoveryOneStepPerInterval: the clock must not
// let two steps land inside one interval.
func TestConcurrencyGovernor_QuietRecoveryOneStepPerInterval(t *testing.T) {
	now := time.Now()
	g := NewConcurrencyGovernor(4, GovernorConfig{RecoveryIntervalSeconds: 60})
	g.SetNow(func() time.Time { return now })

	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}
	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}
	if got := g.Limit(); got != 2 {
		t.Fatalf("setup: limit = %d, want 2", got)
	}

	g.SetNow(func() time.Time { return now.Add(90 * time.Second) })
	if !g.MaybeRecover() {
		t.Fatal("first recovery did not fire")
	}
	if g.MaybeRecover() {
		t.Error("a second recovery fired inside the same interval")
	}
	if got := g.Limit(); got != 3 {
		t.Errorf("limit = %d, want 3 (exactly one step)", got)
	}
}

// TestConcurrencyGovernor_FailureResetsTheQuietClock: pressure during recovery
// must postpone the next step rather than being ignored.
func TestConcurrencyGovernor_FailureResetsTheQuietClock(t *testing.T) {
	now := time.Now()
	g := NewConcurrencyGovernor(4, GovernorConfig{RecoveryIntervalSeconds: 300})
	g.SetNow(func() time.Time { return now })

	for i := 0; i < 8; i++ {
		g.RecordOutcome(false)
	}

	// Four quiet minutes, then fresh pressure.
	g.SetNow(func() time.Time { return now.Add(4 * time.Minute) })
	g.RecordOutcome(false)

	// Two more minutes: six since the cut, but only two since the failure.
	g.SetNow(func() time.Time { return now.Add(6 * time.Minute) })
	if g.MaybeRecover() {
		t.Error("recovered two minutes after a failure; the clock did not reset")
	}

	g.SetNow(func() time.Time { return now.Add(10 * time.Minute) })
	if !g.MaybeRecover() {
		t.Error("no recovery a full interval after the last failure")
	}
}
