package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestScheduler_RetryQueueSkipsBeadsWithLiveWorkers guards the consequence of no
// longer clearing retry state on spawn. Retry state now outlives the spawn, so a
// bead being worked on right now is still in the retry snapshot; queueing it
// would race a second worker against the live one, and the spawner's refusal
// would then be recorded as a further failure of a bead that is fine.
func TestScheduler_RetryQueueSkipsBeadsWithLiveWorkers(t *testing.T) {
	s, o := newTestScheduler(t, 5, GovernorConfig{})

	bead := types.Bead{ID: types.BeadID("bead-live"), Status: types.StatusOpen}
	if err := o.dag.AddNode(bead); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// The bead failed once, long enough ago that the cooldown has passed.
	o.retryManager.RecordRetry("bead-live", infraErr())
	backdate(t, o.retryManager, "bead-live", time.Hour)

	// With no worker running, the bead is eligible for retry.
	if got := s.getRetryEligibleBeads(nil); len(got) != 1 {
		t.Fatalf("eligible beads with no live worker = %d, want 1", len(got))
	}

	// With a worker already on it, it must not be queued again.
	o.workerMutex.Lock()
	o.activeWorkers["bead-live"] = &types.Worker{ID: "bead-live", BeadID: bead.ID}
	o.workerMutex.Unlock()

	if got := s.getRetryEligibleBeads(nil); len(got) != 0 {
		t.Errorf("bead was queued for retry while a worker was still running on it (got %d)", len(got))
	}
}

// TestScheduler_RetryStateSurvivesUntilCompletion is the regression test for the
// defect itself: retry counts must accumulate across attempts. Clearing on spawn
// reset the count every time, so it never reached 2 — the maxRetries cap never
// fired once, and beads that genuinely could not build were retried forever.
func TestScheduler_RetryStateSurvivesUntilCompletion(t *testing.T) {
	_, o := newTestScheduler(t, 5, GovernorConfig{})

	for i := 1; i <= 3; i++ {
		o.retryManager.RecordRetry("bead-bad", faultErr())
		if got := o.retryManager.GetRetryCount("bead-bad"); got != i {
			t.Fatalf("after %d failures the fault count is %d — retry state is being reset between attempts", i, got)
		}
	}

	backdate(t, o.retryManager, "bead-bad", time.Hour)
	if o.retryManager.ShouldRetry("bead-bad", 3) {
		t.Error("a bead with 3 genuine faults is still eligible; the cap is not firing")
	}

	// Only a real completion clears the state.
	o.retryManager.ClearRetry("bead-bad")
	if !o.retryManager.ShouldRetry("bead-bad", 3) {
		t.Error("bead should be eligible again after its state was cleared on completion")
	}
}
