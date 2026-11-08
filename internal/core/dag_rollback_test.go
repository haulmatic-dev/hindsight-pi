package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED contract tests for dependent-release rollback on rejected close
// (bead hydra-63bh). The behavior under test:
//
//  1. Worker completion of B marks B completed and releases its dependent D
//     (D becomes ready).
//  2. Rejecting a bead whose completion released no dependents emits no event.
//  3. Rollback is idempotent: applying it twice emits nothing new.
//  4. A bead that closes verified keeps its dependents released — no rollback,
//     no event.
//
// dagRollbackRequireImpl skips loudly while the GREEN implementation is
// absent and fails under DAG_ROLLBACK_ENFORCE=1, mirroring the
// BEAD_CLAIMS_ENFORCE / STALE_FILING_ENFORCE RED gates.

func dagRollbackRequireImpl(t *testing.T) {
	t.Helper()
	if !dagRollbackImplemented {
		if os.Getenv("DAG_ROLLBACK_ENFORCE") == "" {
			t.Skip("RED: dependent-release rollback contract not yet implemented (set DAG_ROLLBACK_ENFORCE=1 to fail)")
		}
		t.Fatal("RED: DAG_ROLLBACK_ENFORCE=1 but dependent-release rollback contract not implemented")
	}
}

// dagRollbackGraph builds a DependencyGraph over the given beads.
func dagRollbackGraph(t *testing.T, beads []types.Bead) *DependencyGraph {
	t.Helper()
	g := NewDependencyGraph()
	if err := g.BuildFromBeads(beads); err != nil {
		t.Fatalf("BuildFromBeads: %v", err)
	}
	return g
}

// dagRollbackEventCount counts "release_rolled_back" events in the sink's
// central stream. A missing stream file counts as zero events.
func dagRollbackEventCount(t *testing.T, sink *EventSink) int {
	t.Helper()
	path := filepath.Join(sink.centralDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	events, err := ReadEvents(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("ReadEvents: %v", err)
	}
	n := 0
	for _, ev := range events {
		if ev.Type == "release_rolled_back" {
			n++
		}
	}
	return n
}

// dagRollbackBeadInList reports whether beadID appears in list.
func dagRollbackBeadInList(t *testing.T, list []types.Bead, beadID string) bool {
	t.Helper()
	for _, b := range list {
		if string(b.ID) == beadID {
			return true
		}
	}
	return false
}

func dagRollbackIsReady(t *testing.T, g *DependencyGraph, beadID string) bool {
	t.Helper()
	return dagRollbackBeadInList(t, g.GetReadyBeads(), beadID)
}

func dagRollbackIsBlocked(t *testing.T, g *DependencyGraph, beadID string) bool {
	t.Helper()
	return dagRollbackBeadInList(t, g.GetBlockedBeads(), beadID)
}

// TestDagRollback_CompletionReleasesDependent: worker completion of B marks B
// completed and releases dependent D (D becomes ready). Pins the release
// baseline the rollback contract builds on.
func TestDagRollback_CompletionReleasesDependent(t *testing.T) {
	beads := []types.Bead{
		{ID: "bead-b", Status: types.StatusOpen},
		{ID: "bead-d", Status: types.StatusOpen, Dependencies: []types.BeadID{"bead-b"}},
	}
	g := dagRollbackGraph(t, beads)

	newlyReady, err := g.MarkCompleted("bead-b")
	if err != nil {
		t.Fatalf("MarkCompleted(bead-b): %v", err)
	}
	status, err := g.dag.GetNodeStatus("bead-b")
	if err != nil || status != types.StatusCompleted {
		t.Fatalf("B status after completion = %v (err %v), want completed", status, err)
	}
	if len(newlyReady) != 1 || newlyReady[0] != "bead-d" {
		t.Fatalf("newlyReady = %v, want [bead-d]", newlyReady)
	}
	if !dagRollbackIsReady(t, g, "bead-d") {
		t.Fatal("dependent D not released (not ready) after B completed")
	}
}

// TestDagRollback_RejectedCloseRollsBackReleasedDependent: after B's close is
// rejected, the rollback re-blocks its released dependent D and emits exactly
// one typed event.
func TestDagRollback_RejectedCloseRollsBackReleasedDependent(t *testing.T) {
	dagRollbackRequireImpl(t)

	beads := []types.Bead{
		{ID: "bead-b", Status: types.StatusOpen},
		{ID: "bead-d", Status: types.StatusOpen, Dependencies: []types.BeadID{"bead-b"}},
	}
	g := dagRollbackGraph(t, beads)

	if _, err := g.MarkCompleted("bead-b"); err != nil {
		t.Fatalf("MarkCompleted(bead-b): %v", err)
	}

	sink := NewEventSink(t.TempDir())
	rolledBack := RollbackReleasedDependents(g, sink, "proj", "bead-b")
	if len(rolledBack) != 1 || rolledBack[0] != "bead-d" {
		t.Fatalf("rolledBack = %v, want [bead-d]", rolledBack)
	}
	if n := dagRollbackEventCount(t, sink); n != 1 {
		t.Fatalf("release_rolled_back events after rollback = %d, want 1", n)
	}
	if !dagRollbackIsBlocked(t, g, "bead-d") || dagRollbackIsReady(t, g, "bead-d") {
		t.Fatal("dependent D not re-blocked after rollback")
	}
}

// TestDagRollback_RejectNoReleasedDependentsEmitsNoEvent: rejecting a bead
// whose completion released no dependents (its only dependent is still blocked
// by another open dependency) must emit no rollback event and leave the
// dependent untouched.
func TestDagRollback_RejectNoReleasedDependentsEmitsNoEvent(t *testing.T) {
	dagRollbackRequireImpl(t)

	beads := []types.Bead{
		{ID: "bead-b1", Status: types.StatusOpen},
		{ID: "bead-b2", Status: types.StatusOpen},
		{ID: "bead-d", Status: types.StatusOpen, Dependencies: []types.BeadID{"bead-b1", "bead-b2"}},
	}
	g := dagRollbackGraph(t, beads)

	newlyReady, err := g.MarkCompleted("bead-b1")
	if err != nil {
		t.Fatalf("MarkCompleted(bead-b1): %v", err)
	}
	if len(newlyReady) != 0 {
		t.Fatalf("newlyReady = %v, want none (D still blocked by bead-b2)", newlyReady)
	}

	sink := NewEventSink(t.TempDir())
	if got := RollbackReleasedDependents(g, sink, "proj", "bead-b1"); len(got) != 0 {
		t.Fatalf("rolledBack = %v, want none", got)
	}
	if n := dagRollbackEventCount(t, sink); n != 0 {
		t.Fatalf("release_rolled_back events = %d, want 0 (no released dependents)", n)
	}
	if !dagRollbackIsBlocked(t, g, "bead-d") {
		t.Fatal("dependent D must stay blocked when it was never released")
	}
}

// TestDagRollback_RollbackIdempotent: rolling back the same completion twice
// re-blocks D once; the second rollback emits nothing new and changes nothing.
func TestDagRollback_RollbackIdempotent(t *testing.T) {
	dagRollbackRequireImpl(t)

	beads := []types.Bead{
		{ID: "bead-b", Status: types.StatusOpen},
		{ID: "bead-d", Status: types.StatusOpen, Dependencies: []types.BeadID{"bead-b"}},
	}
	g := dagRollbackGraph(t, beads)

	if _, err := g.MarkCompleted("bead-b"); err != nil {
		t.Fatalf("MarkCompleted(bead-b): %v", err)
	}

	sink := NewEventSink(t.TempDir())
	first := RollbackReleasedDependents(g, sink, "proj", "bead-b")
	if len(first) != 1 || first[0] != "bead-d" {
		t.Fatalf("first rolledBack = %v, want [bead-d]", first)
	}
	if n := dagRollbackEventCount(t, sink); n != 1 {
		t.Fatalf("release_rolled_back events after first rollback = %d, want 1", n)
	}

	second := RollbackReleasedDependents(g, sink, "proj", "bead-b")
	if len(second) != 0 {
		t.Fatalf("second rolledBack = %v, want none", second)
	}
	if n := dagRollbackEventCount(t, sink); n != 1 {
		t.Fatalf("release_rolled_back events after second rollback = %d, want still 1", n)
	}
	if !dagRollbackIsBlocked(t, g, "bead-d") || dagRollbackIsReady(t, g, "bead-d") {
		t.Fatal("dependent D must stay blocked after idempotent second rollback")
	}
}

// TestDagRollback_VerifiedCloseKeepsDependentsReleased: a bead that closes
// verified performs no rollback — its dependent stays released and no
// rollback event exists.
func TestDagRollback_VerifiedCloseKeepsDependentsReleased(t *testing.T) {
	dagRollbackRequireImpl(t)

	beads := []types.Bead{
		{ID: "bead-b", Status: types.StatusOpen},
		{ID: "bead-d", Status: types.StatusOpen, Dependencies: []types.BeadID{"bead-b"}},
	}
	g := dagRollbackGraph(t, beads)

	if _, err := g.MarkCompleted("bead-b"); err != nil {
		t.Fatalf("MarkCompleted(bead-b): %v", err)
	}

	sink := NewEventSink(t.TempDir())
	if n := dagRollbackEventCount(t, sink); n != 0 {
		t.Fatalf("release_rolled_back events on verified close = %d, want 0", n)
	}
	if !dagRollbackIsReady(t, g, "bead-d") || dagRollbackIsBlocked(t, g, "bead-d") {
		t.Fatal("dependent D must stay released when B closes verified")
	}
}
