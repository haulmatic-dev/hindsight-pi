package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestScheduler_RetryQueueSkipsHumanGates guards the retry path that used to
// resurrect approval-gate, open-approval, and epic beads into the spawn loop.
// Human gates and container beads must never be queued for retry, and any
// stale retry state for them must be cleared so it stops burning scheduler
// turns.
func TestScheduler_RetryQueueSkipsHumanGates(t *testing.T) {
	s, o := newTestScheduler(t, 5, GovernorConfig{})

	cases := []struct {
		id  string
		typ string
	}{
		{"gate-1", "approval-gate"},
		{"approval-1", "approval"},
		{"epic-1", "epic"},
	}

	for _, tc := range cases {
		bead := types.Bead{ID: types.BeadID(tc.id), Type: tc.typ, Status: types.StatusOpen}
		if err := o.dag.AddNode(bead); err != nil {
			t.Fatalf("AddNode %s: %v", tc.id, err)
		}
		o.retryManager.RecordRetry(tc.id, faultErr())
		backdate(t, o.retryManager, tc.id, time.Hour)
	}

	got := s.getRetryEligibleBeads(nil)
	if len(got) != 0 {
		t.Fatalf("human/container beads queued for retry: got %d, want 0", len(got))
	}

	for _, tc := range cases {
		if o.retryManager.GetRetryCount(tc.id) != 0 {
			t.Errorf("retry state for %s not cleared", tc.id)
		}
	}
}

// TestScheduler_RetryQueueFiltersHumanGatesFromReadySet ensures that even if
// an external discovery source returns a human gate or container bead in the
// ready set, it is stripped before the spawn loop sees it.
func TestScheduler_RetryQueueFiltersHumanGatesFromReadySet(t *testing.T) {
	s, o := newTestScheduler(t, 5, GovernorConfig{})

	ready := []types.Bead{
		{ID: "task-1", Type: "implement", Status: types.StatusOpen},
		{ID: "gate-1", Type: "approval-gate", Status: types.StatusOpen},
		{ID: "approval-1", Type: "approval", Status: types.StatusOpen},
		{ID: "epic-1", Type: "epic", Status: types.StatusOpen},
	}

	o.retryManager.RecordRetry("gate-1", faultErr())
	backdate(t, o.retryManager, "gate-1", time.Hour)

	got := s.getRetryEligibleBeads(ready)
	if len(got) != 1 {
		t.Fatalf("ready set after filter = %d, want 1", len(got))
	}
	if string(got[0].ID) != "task-1" {
		t.Errorf("unexpected ready bead: %s", got[0].ID)
	}
	if o.retryManager.GetRetryCount("gate-1") != 0 {
		t.Error("retry state for gate-1 not cleared")
	}
}

// TestScheduler_ShouldSpawnWorker_CaseInsensitiveHumanGates verifies that
// human gate types are rejected regardless of casing or surrounding
// whitespace.
func TestScheduler_ShouldSpawnWorker_CaseInsensitiveHumanGates(t *testing.T) {
	s, _ := newTestScheduler(t, 5, GovernorConfig{})

	for _, typ := range []string{"approval-gate", "Approval-Gate", " approval-gate ", "approval", "Approval", "epic", "Epic"} {
		node := &Node{Bead: types.Bead{ID: types.BeadID("b-" + typ), Type: typ}}
		if s.ShouldSpawnWorker(node) {
			t.Errorf("ShouldSpawnWorker returned true for type %q", typ)
		}
	}

	if !s.ShouldSpawnWorker(&Node{Bead: types.Bead{ID: "task-1", Type: "implement"}}) {
		t.Error("ShouldSpawnWorker returned false for executable bead")
	}
}
