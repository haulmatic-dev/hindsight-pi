package core

import (
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// newOrphanClaimScheduler builds a scheduler whose overseer has a mock beads
// store, one project, and an event sink pointed at a temp dir.
func newOrphanClaimScheduler(t *testing.T) (*Scheduler, *Overseer, string) {
	t.Helper()
	s, o := newTestScheduler(t, 5, GovernorConfig{})
	o.beadsClient = newMockBeadsClient()
	eventsDir := t.TempDir()
	o.beadCloser.SetEventSink(NewEventSink(eventsDir))
	o.projects = []types.Project{{Name: "proj", Path: "/tmp/fake/proj", Type: types.RepoTypeSingle}}
	o.beadAnalyzer = NewBeadAnalyzer(o.projects)
	return s, o, eventsDir
}

// TestScheduler_OrphanClaimReset_ReopensInProgressBead guards the fms wedge
// (2026-10-01): a worker claimed a bead (in_progress), exhausted retries, and
// the bead was left invisible to the scheduler (not ready) AND blocking all
// dependents (not closed) — a 17-bead DAG ladder wedged for 1h+. The
// reconciliation must reset the stale claim to open, clear the retry entry,
// and emit exactly one orphan_claim_reset event.
func TestScheduler_OrphanClaimReset_ReopensInProgressBead(t *testing.T) {
	s, o, eventsDir := newOrphanClaimScheduler(t)

	bead := types.Bead{ID: types.BeadID("proj-orphan"), Project: "proj", Status: types.StatusInProgress, Type: "bug"}
	o.beadsClient.(*mockBeadsClient).addBead(bead)
	if err := o.dag.AddNode(bead); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	for i := 0; i < 3; i++ {
		o.retryManager.RecordRetry(string(bead.ID), faultErr())
	}

	if got := s.getRetryEligibleBeads(nil); len(got) != 0 {
		t.Fatalf("exhausted in_progress bead should not be scheduled directly, got %d", len(got))
	}

	mock := o.beadsClient.(*mockBeadsClient)
	mock.mu.RLock()
	status := mock.beads[string(bead.ID)].Status
	mock.mu.RUnlock()
	if status != types.StatusOpen {
		t.Errorf("orphaned in_progress bead was not reset to open (status %s)", status)
	}
	if o.retryManager.GetRetryCount(string(bead.ID)) != 0 {
		t.Error("retry entry was not cleared on orphan-claim reset")
	}

	// Second tick must not emit a second event (no retry entry left, but the
	// guard is once-per-exhaustion either way).
	_ = s.getRetryEligibleBeads(nil)

	events, err := ReadEvents(filepath.Join(eventsDir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	n := 0
	for _, e := range events {
		if e.Type == "orphan_claim_reset" {
			n++
			if e.Bead != string(bead.ID) {
				t.Errorf("event bead = %q, want %q", e.Bead, bead.ID)
			}
			if e.Fields["last_worker"] == nil {
				t.Error("orphan_claim_reset event missing last_worker field")
			}
		}
	}
	if n != 1 {
		t.Errorf("expected exactly one orphan_claim_reset event, got %d", n)
	}
}

// TestScheduler_OrphanClaimReset_SkipsHardParked ensures the reconciliation
// never respawns a hard-parked death: escalated, worktree-unavailable and
// integration-exhausted beads stay as-is.
func TestScheduler_OrphanClaimReset_SkipsHardParked(t *testing.T) {
	s, o, _ := newOrphanClaimScheduler(t)

	for _, tc := range []struct{ label string }{
		{escalatedLabel}, {worktreeUnavailableLabel}, {integrationExhaustedLabel},
	} {
		id := types.BeadID("proj-" + strings.ReplaceAll(tc.label, "-", ""))
		bead := types.Bead{ID: id, Project: "proj", Status: types.StatusInProgress, Labels: []string{tc.label}}
		o.beadsClient.(*mockBeadsClient).addBead(bead)
		if err := o.dag.AddNode(bead); err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		for i := 0; i < 3; i++ {
			o.retryManager.RecordRetry(string(id), faultErr())
		}
	}

	_ = s.getRetryEligibleBeads(nil)

	mock := o.beadsClient.(*mockBeadsClient)
	mock.mu.RLock()
	defer mock.mu.RUnlock()
	for _, tc := range []struct{ label string }{
		{escalatedLabel}, {worktreeUnavailableLabel}, {integrationExhaustedLabel},
	} {
		id := "proj-" + strings.ReplaceAll(tc.label, "-", "")
		if st := mock.beads[id].Status; st != types.StatusInProgress {
			t.Errorf("hard-parked bead %s (label %s) was reset to %s; must stay in_progress", id, tc.label, st)
		}
	}
}

// TestScheduler_OrphanClaimReset_SkipsOpenBeads: reconciliation only fires for
// genuinely orphaned claims (in_progress); open/other statuses untouched.
func TestScheduler_OrphanClaimReset_SkipsOpenBeads(t *testing.T) {
	s, o, _ := newOrphanClaimScheduler(t)

	bead := types.Bead{ID: types.BeadID("proj-open"), Project: "proj", Status: types.StatusOpen}
	o.beadsClient.(*mockBeadsClient).addBead(bead)
	if err := o.dag.AddNode(bead); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	for i := 0; i < 3; i++ {
		o.retryManager.RecordRetry(string(bead.ID), faultErr())
	}

	_ = s.getRetryEligibleBeads(nil)
	if o.retryManager.GetRetryCount(string(bead.ID)) == 0 {
		t.Error("non-orphan open bead had its retry entry cleared by the orphan-claim reconciler")
	}
	_ = io.Discard
	_ = log.Writer
}
