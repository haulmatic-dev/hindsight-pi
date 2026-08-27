package core

import (
	"path/filepath"
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

// TestScheduler_ExhaustedBeadRemovedFromReadyAndEventEmittedOnce guards the
// 2026-08-27 runaway where a DAG-ready, retry-exhausted bead was spawned every
// tick, producing nested errors and growing the attempt counter to 386/3.
// Such beads must be dropped from the ready list and emit exactly one
// retry_exhausted event.
func TestScheduler_ExhaustedBeadRemovedFromReadyAndEventEmittedOnce(t *testing.T) {
	s, o := newTestScheduler(t, 5, GovernorConfig{})

	eventsDir := t.TempDir()
	o.beadCloser.SetEventSink(NewEventSink(eventsDir))

	bead := types.Bead{ID: types.BeadID("bead-exhausted"), Project: "proj", Type: "bug"}
	for i := 0; i < 3; i++ {
		o.retryManager.RecordRetry(string(bead.ID), faultErr())
	}

	ready := []types.Bead{bead}
	got := s.getRetryEligibleBeads(ready)
	if len(got) != 0 {
		t.Fatalf("exhausted bead still scheduled: got %d ready beads, want 0", len(got))
	}

	// A second tick must not emit another event.
	_ = s.getRetryEligibleBeads(ready)

	events, err := ReadEvents(filepath.Join(eventsDir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one retry_exhausted event, got %d", len(events))
	}
	if events[0].Type != "retry_exhausted" {
		t.Errorf("event type = %q, want retry_exhausted", events[0].Type)
	}
	if events[0].Bead != string(bead.ID) {
		t.Errorf("event bead = %q, want %q", events[0].Bead, bead.ID)
	}
}
// allowed to react to. Exit 7 is the one that matters: it is a capacity blip
// that clears in seconds, and feeding it to the governor made the governor and
// the provider pool two controllers fighting over one actuator.
func TestGovernorPressureCodes(t *testing.T) {
	pressure := map[int]bool{
		2: true,  // 429 rate limit — sustained pressure
		3: true,  // timeout — sustained pressure
		7: false, // transient overload — clears in seconds, not our concurrency
		6: false, // quota exhausted — no amount of slowing down helps
		4: false, // infra
		1: false, // code fault
		5: false, // stub
	}
	for code, want := range pressure {
		if got := isConcurrencyPressureExitCode(code); got != want {
			t.Errorf("isConcurrencyPressureExitCode(%d) = %v, want %v", code, got, want)
		}
	}
}
