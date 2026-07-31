package core

import (
	"errors"
	"testing"
	"time"
)

// backdate makes a bead's last failure look old enough that the cooldown has
// passed, so a test can assert on the budget rules without waiting a minute.
func backdate(t *testing.T, rm *RetryManager, beadID string, age time.Duration) {
	t.Helper()
	rm.retriesMu.Lock()
	defer rm.retriesMu.Unlock()
	info, ok := rm.retries[beadID]
	if !ok {
		t.Fatalf("bead %s has no retry state to backdate", beadID)
	}
	info.lastError = time.Now().Add(-age)
}

func infraErr() error { return errors.New("infrastructure failure (exit 7)") }
func faultErr() error { return errors.New("worker exited with code 1") }

func newTestRetryManager() *RetryManager {
	return NewRetryManager(5*time.Second, 2*time.Minute)
}

// TestRetryManager_InfraFailuresNeverExhaustTheBudget is the policy that matters
// most: a provider overload is not evidence that the bead is broken. On
// 2026-07-31, 30 of 39 worker failures were exit 7 — counting those against the
// bead would abandon work that was never at fault.
func TestRetryManager_InfraFailuresNeverExhaustTheBudget(t *testing.T) {
	rm := newTestRetryManager()

	for i := 0; i < 10; i++ {
		rm.RecordRetry("b1", infraErr())
	}

	if got := rm.GetRetryCount("b1"); got != 0 {
		t.Errorf("fault count = %d after 10 infra failures, want 0", got)
	}
	if got := rm.GetInfraRetryCount("b1"); got != 10 {
		t.Errorf("infra count = %d, want 10", got)
	}

	backdate(t, rm, "b1", time.Hour)
	if !rm.ShouldRetry("b1", 3) {
		t.Error("bead was abandoned after infra-only failures; infra must never exhaust the budget")
	}
}

// TestRetryManager_CodeFaultsExhaustTheBudget is the other half of the policy:
// a bead that genuinely fails to build must stop after maxRetries rather than
// being retried forever.
func TestRetryManager_CodeFaultsExhaustTheBudget(t *testing.T) {
	rm := newTestRetryManager()

	for i := 0; i < 3; i++ {
		rm.RecordRetry("b2", faultErr())
	}
	backdate(t, rm, "b2", time.Hour)

	if got := rm.GetRetryCount("b2"); got != 3 {
		t.Fatalf("fault count = %d, want 3", got)
	}
	if rm.ShouldRetry("b2", 3) {
		t.Error("bead retried past the 3-fault cap")
	}
}

// TestRetryManager_MixedFailuresCountOnlyFaults pins the boundary: infra noise
// interleaved with real faults must not shift where the cap falls.
func TestRetryManager_MixedFailuresCountOnlyFaults(t *testing.T) {
	rm := newTestRetryManager()

	rm.RecordRetry("b3", infraErr())
	rm.RecordRetry("b3", faultErr())
	rm.RecordRetry("b3", infraErr())
	rm.RecordRetry("b3", faultErr())
	backdate(t, rm, "b3", time.Hour)

	if !rm.ShouldRetry("b3", 3) {
		t.Fatal("2 faults + 2 infra should still be retryable under a 3-fault cap")
	}

	rm.RecordRetry("b3", faultErr())
	backdate(t, rm, "b3", time.Hour)
	if rm.ShouldRetry("b3", 3) {
		t.Error("the third genuine fault should have exhausted the budget")
	}
}

// TestRetryManager_InfraThrottleDropsToHourly: unlimited infra retries must not
// mean unlimited infra retries per tick. Past the ceiling a bead checks hourly.
func TestRetryManager_InfraThrottleDropsToHourly(t *testing.T) {
	rm := newTestRetryManager()

	for i := 0; i < maxInfraRetries; i++ {
		rm.RecordRetry("b4", infraErr())
	}

	backdate(t, rm, "b4", 30*time.Minute)
	if rm.ShouldRetry("b4", 3) {
		t.Error("past the infra ceiling a bead must wait a full hour, not 30 minutes")
	}

	backdate(t, rm, "b4", 61*time.Minute)
	if !rm.ShouldRetry("b4", 3) {
		t.Error("after an hour the bead must become eligible again")
	}
}

// TestRetryManager_CooldownIsEnforced guards the pacing itself: a bead that just
// failed must not be respawned on the next ten-second tick.
func TestRetryManager_CooldownIsEnforced(t *testing.T) {
	rm := newTestRetryManager()
	rm.RecordRetry("b5", infraErr())

	if rm.ShouldRetry("b5", 3) {
		t.Error("a bead that failed a moment ago was immediately eligible again")
	}

	backdate(t, rm, "b5", defaultRetryCooldown+10*time.Minute)
	if !rm.ShouldRetry("b5", 3) {
		t.Error("bead still blocked long after the cooldown and backoff elapsed")
	}
}

// TestRetryManager_BackoffGrowsWithTotalAttempts: infra failures pace retries
// even though they do not exhaust them, so an outage costs one attempt per
// window instead of one per tick.
func TestRetryManager_BackoffGrowsWithTotalAttempts(t *testing.T) {
	rm := newTestRetryManager()

	first := rm.RecordRetry("b6", infraErr())
	second := rm.RecordRetry("b6", infraErr())
	third := rm.RecordRetry("b6", infraErr())

	if !(first < second && second < third) {
		t.Errorf("backoff did not grow across attempts: %v, %v, %v", first, second, third)
	}

	for i := 0; i < 20; i++ {
		rm.RecordRetry("b6", infraErr())
	}
	if got := rm.RecordRetry("b6", infraErr()); got > rm.MaxDelay() {
		t.Errorf("backoff %v exceeded the configured cap %v", got, rm.MaxDelay())
	}
}

// TestRetryManager_UnknownBeadIsRetryable keeps the first-attempt path honest:
// a bead with no history must not be blocked.
func TestRetryManager_UnknownBeadIsRetryable(t *testing.T) {
	rm := newTestRetryManager()
	if !rm.ShouldRetry("never-seen", 3) {
		t.Error("a bead with no retry history must be eligible")
	}
}

// TestRetryManager_ClearRetryResetsBothCounters: only a real completion clears
// state, and when it does it must clear all of it.
func TestRetryManager_ClearRetryResetsBothCounters(t *testing.T) {
	rm := newTestRetryManager()
	rm.RecordRetry("b7", faultErr())
	rm.RecordRetry("b7", infraErr())

	rm.ClearRetry("b7")

	if got := rm.GetRetryCount("b7"); got != 0 {
		t.Errorf("fault count = %d after ClearRetry, want 0", got)
	}
	if got := rm.GetInfraRetryCount("b7"); got != 0 {
		t.Errorf("infra count = %d after ClearRetry, want 0", got)
	}
	if !rm.ShouldRetry("b7", 3) {
		t.Error("bead should be freshly eligible after its state was cleared")
	}
}
