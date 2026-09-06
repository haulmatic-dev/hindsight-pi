package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestGetReadyBeads_ExcludesEscalated guards the scheduler-side half of the
// churn-loop fix: even if an escalated bead ever shows up as DAG-ready (e.g.
// a stale status refresh raced the label write), GetReadyBeads must not hand
// it to a worker.
func TestGetReadyBeads_ExcludesEscalated(t *testing.T) {
	dag := NewDAG()

	escalated := types.Bead{
		ID:       types.BeadID("proj-esc-ready"),
		Project:  "proj",
		Title:    "escalated but apparently ready",
		Type:     "implement",
		Status:   types.StatusOpen,
		Priority: 1,
		Labels:   []string{"needs-human", "escalated"},
	}
	normal := types.Bead{
		ID:       types.BeadID("proj-normal-ready"),
		Project:  "proj",
		Title:    "normal ready bead",
		Type:     "implement",
		Status:   types.StatusOpen,
		Priority: 2,
	}
	if err := dag.AddNode(escalated); err != nil {
		t.Fatalf("AddNode(escalated): %v", err)
	}
	if err := dag.AddNode(normal); err != nil {
		t.Fatalf("AddNode(normal): %v", err)
	}

	nav := NewDAGNavigator(dag)
	ready := nav.GetReadyBeads(nil)

	if len(ready) != 1 {
		t.Fatalf("ready = %d beads, want 1 (escalated excluded); got: %+v", len(ready), ready)
	}
	if ready[0].ID != normal.ID {
		t.Fatalf("ready[0] = %s, want %s", ready[0].ID, normal.ID)
	}

	// Session-completed map must still work alongside the label filter.
	ready = nav.GetReadyBeads(map[string]bool{string(normal.ID): true})
	if len(ready) != 0 {
		t.Fatalf("ready = %d after completing normal bead, want 0", len(ready))
	}
}
