package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// The event-driven scheduling path (TriggerImmediateSchedule ->
// Scheduler.GetReadyNodes -> DAG.GetReadyNodes) returned ready nodes in Go
// map-iteration order — no priority sort. The periodic tick path sorts via
// DAGNavigator.GetReadyBeads, but the immediate path bypassed it, so a spawn
// triggered by a chain-completion event picked a random ready bead: on
// srv-smart-engine (2026-08-14) P3 validation churn (tftg, 0mt2j, yvvc) took
// worker slots while 12 ready P1 chain beads waited.
//
// Rule: Scheduler.GetReadyNodes must return nodes sorted by priority
// descending, matching the tick path's contract (AGENTS.md: GetReadyBeadsFromDAG
// sorts by priority descending).
func TestSchedulerGetReadyNodes_SortsByPriorityDescending(t *testing.T) {
	config := DefaultConfig()
	o := NewOverseer(config)

	dag := NewDAG()
	// Insert low priority first so map order cannot accidentally satisfy the
	// assertion: P3, then P0, then P1.
	for _, b := range []types.Bead{
		{ID: "bead-p3", Title: "churn", Priority: 3, Status: types.StatusOpen, Type: "fix"},
		{ID: "bead-p0", Title: "critical", Priority: 0, Status: types.StatusOpen, Type: "task"},
		{ID: "bead-p1", Title: "real work", Priority: 1, Status: types.StatusOpen, Type: "task"},
	} {
		if err := dag.AddNode(b); err != nil {
			t.Fatalf("AddNode %s: %v", b.ID, err)
		}
	}

	o.dagMu.Lock()
	o.dag = dag
	o.dagMu.Unlock()

	nodes := o.scheduler.GetReadyNodes()
	if len(nodes) != 3 {
		t.Fatalf("ready nodes = %d, want 3", len(nodes))
	}
	want := []types.BeadID{"bead-p0", "bead-p1", "bead-p3"}
	for i, w := range want {
		if nodes[i].Bead.ID != w {
			t.Errorf("position %d = %s (P%d), want %s — immediate schedule path must sort by priority descending",
				i, nodes[i].Bead.ID, nodes[i].Bead.Priority, w)
		}
	}
}
