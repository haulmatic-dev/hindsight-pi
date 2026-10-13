package core

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func hardParkTestProject(t *testing.T) []types.Project {
	t.Helper()
	return []types.Project{{Name: "proj", Path: filepath.Join(t.TempDir(), "proj")}}
}

func hardParkBead(id string, labels ...string) types.Bead {
	return types.Bead{ID: types.BeadID(id), Project: "proj", Title: "t", Labels: labels}
}

func TestReconcileHardParkedWorktrees_Hysteresis(t *testing.T) {
	bc := &mockBeadsClient{beads: map[string]types.Bead{}}
	bc.addBead(hardParkBead("proj-hh1", worktreeUnavailableLabel))
	projects := hardParkTestProject(t)
	consecutive := map[string]int{}
	calls := 0
	probe := func(string) error { calls++; return nil }

	unparked := ReconcileHardParkedWorktrees(bc, projects, probe, consecutive, nil)
	if len(unparked) != 0 {
		t.Fatalf("first successful probe must NOT clear the label, unparked=%v", unparked)
	}
	if bc.beads["proj-hh1"].Labels[0] != worktreeUnavailableLabel {
		t.Fatalf("label removed after only 1 successful probe")
	}
	unparked = ReconcileHardParkedWorktrees(bc, projects, probe, consecutive, nil)
	if len(unparked) != 1 || unparked[0] != "proj-hh1" {
		t.Fatalf("second consecutive successful probe must clear, unparked=%v", unparked)
	}
	if got := bc.beads["proj-hh1"].Labels; len(got) != 0 {
		t.Fatalf("label still present after 2 probes: %v", got)
	}
	if calls != 2 {
		t.Fatalf("expected 2 probes, got %d", calls)
	}
}

func TestReconcileHardParkedWorktrees_EventEmitted(t *testing.T) {
	dir := t.TempDir()
	bc := &mockBeadsClient{beads: map[string]types.Bead{}}
	bc.addBead(hardParkBead("proj-ee1", worktreeUnavailableLabel))
	projects := []types.Project{{Name: "proj", Path: filepath.Join(dir, "proj")}}
	consecutive := map[string]int{"proj-ee1": 1} // one probe already succeeded
	ok := func(string) error { return nil }

	sink := NewEventSink(filepath.Join(dir, "central"))

	ReconcileHardParkedWorktrees(bc, projects, ok, consecutive, sink)

	found := false
	all, err := ReadEvents(filepath.Join(dir, "proj", ".hydra", "events.jsonl"))
	if err != nil {
		t.Fatalf("read repo events: %v", err)
	}
	for _, ev := range all {
		if ev.Type == worktreeUnparkedEvent && ev.Bead == "proj-ee1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("worktree_unparked event not emitted; events=%v", all)
	}
}

func TestReconcileHardParkedWorktrees_ProbeFailureResetsAndFailsOpen(t *testing.T) {
	bc := &mockBeadsClient{beads: map[string]types.Bead{}}
	bc.addBead(hardParkBead("proj-ff1", worktreeUnavailableLabel))
	projects := hardParkTestProject(t)
	consecutive := map[string]int{"proj-ff1": 1} // one success already banked
	fail := func(string) error { return errors.New("git probe failed") }

	unparked := ReconcileHardParkedWorktrees(bc, projects, fail, consecutive, nil)
	if len(unparked) != 0 {
		t.Fatalf("failed probe must not clear labels, unparked=%v", unparked)
	}
	if bc.beads["proj-ff1"].Labels[0] != worktreeUnavailableLabel {
		t.Fatalf("label must stay on failed probe")
	}
	if _, ok := consecutive["proj-ff1"]; ok {
		t.Fatalf("failed probe must reset the hysteresis counter")
	}
}

func TestReconcileHardParkedWorktrees_LabelScopeGuard(t *testing.T) {
	bc := &mockBeadsClient{beads: map[string]types.Bead{}}
	bc.addBead(hardParkBead("proj-gg1", worktreeUnavailableLabel, escalatedLabel, "needs-human"))
	projects := hardParkTestProject(t)
	consecutive := map[string]int{"proj-gg1": 1} // hysteresis already satisfied
	ok := func(string) error { return nil }

	ReconcileHardParkedWorktrees(bc, projects, ok, consecutive, nil)

	got := bc.beads["proj-gg1"].Labels
	if hasBeadLabel(got, worktreeUnavailableLabel) {
		t.Fatalf("worktree-unavailable should be cleared")
	}
	if !hasBeadLabel(got, escalatedLabel) || !hasBeadLabel(got, "needs-human") {
		t.Fatalf("escalated/needs-human must never be cleared: %v", got)
	}
}

func TestReconcileHardParkedWorktrees_NeverTouchesOtherLabels(t *testing.T) {
	bc := &mockBeadsClient{beads: map[string]types.Bead{}}
	bc.addBead(hardParkBead("proj-hn1", escalatedLabel, "needs-human"))
	projects := hardParkTestProject(t)
	consecutive := map[string]int{}
	ok := func(string) error { return nil }

	unparked := ReconcileHardParkedWorktrees(bc, projects, ok, consecutive, nil)
	if len(unparked) != 0 {
		t.Fatalf("no worktree-unavailable label: nothing to clear, unparked=%v", unparked)
	}
}
