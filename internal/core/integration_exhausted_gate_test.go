package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// 2026-09-27 ag6i: a bead whose close repeatedly failed at integrate was
// hard-parked with the integration-exhausted label by failIntegration, but the
// label was only consulted by Charon and the Pan blocked scan — the scheduler
// retry queue, the DAG navigator and bead discovery kept respawning it (~95x,
// each resume re-emitting the verdict in ~30s). These tests pin the label gate
// on every respawn path.

func TestGetReadyBeads_SkipsIntegrationExhausted(t *testing.T) {
	dag := NewDAG()
	g := NewDependencyGraph()
	beads := []types.Bead{
		{ID: "hydra-ok", Title: "ready", Type: "implement", Status: types.StatusOpen},
		{ID: "hydra-ix", Title: "hard-parked", Type: "implement", Status: types.StatusOpen,
			Labels: []string{integrationExhaustedLabel}},
	}
	if err := g.BuildFromBeads(beads); err != nil {
		t.Fatalf("BuildFromBeads: %v", err)
	}
	for _, b := range beads {
		if err := dag.AddNode(b); err != nil {
			t.Fatalf("AddNode %s: %v", b.ID, err)
		}
	}
	nav := NewDAGNavigator(dag)
	ready := nav.GetReadyBeads(nil)
	for _, b := range ready {
		if b.ID == "hydra-ix" {
			t.Fatalf("integration-exhausted bead %s must not be ready (would respawn into failing integrate)", b.ID)
		}
	}
	if len(ready) != 1 || ready[0].ID != "hydra-ok" {
		t.Fatalf("expected only hydra-ok ready, got %v", ready)
	}
}

func TestFilterByDAG_FiltersIntegrationExhausted(t *testing.T) {
	d := &BeadDiscovery{} // nil dag: the escalated/integration gate runs before the dag==nil return
	candidates := []types.Bead{
		{ID: "hydra-ok", Title: "normal", Type: "implement", Status: types.StatusOpen},
		{ID: "hydra-ix", Title: "hard-parked", Type: "implement", Status: types.StatusOpen,
			Labels: []string{integrationExhaustedLabel}},
	}
	kept := d.filterByDAG(candidates)
	for _, b := range kept {
		if b.ID == "hydra-ix" {
			t.Fatalf("discovery must not resurrect integration-exhausted bead %s", b.ID)
		}
	}
	if len(kept) != 1 || kept[0].ID != "hydra-ok" {
		t.Fatalf("expected only hydra-ok kept, got %v", kept)
	}
}
