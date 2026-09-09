package core

import (
	"sync"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Repro for gybo4 (2026-09-09): a bead whose close was rejected/escalated
// stayed in the overseer's in-memory completed set, so the scheduler logged
// "Filtered out 1 beads: ... completed=1" every tick and never spawned it.
// Daemon restart cleared the set and the bead spawned instantly. The set is a
// cache of "closed in the store" and must be re-validated against store state.
func TestFindFromBR_PrunesStaleCompletedEntry(t *testing.T) {
	mock := newMockBeadsClient()
	mock.beads["proj-esc"] = types.Bead{
		ID:      "proj-esc",
		Project: "proj",
		Type:    "task",
		Status:  types.StatusInProgress, // escalated beads are left in_progress
	}

	projects := []types.Project{{Name: "proj", Path: "/projects/proj"}}
	completed := map[string]bool{"proj-esc": true}
	var mu sync.RWMutex
	d := NewBeadDiscovery(mock, nil, projects, completed, nil, &mu)

	beads, err := d.findFromBR(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBR failed: %v", err)
	}
	if len(beads) != 1 || beads[0].ID != "proj-esc" {
		t.Fatalf("expected escalated bead to be schedulable, got %d beads: %+v", len(beads), beads)
	}
	if completed["proj-esc"] {
		t.Fatalf("stale completed entry for escalated bead was not pruned")
	}
}

// The re-validation must not weaken the filter for beads that really are
// closed: a closed bead in the completed set stays filtered.
func TestFindFromBR_KeepsCompletedFilterForClosedBead(t *testing.T) {
	mock := newMockBeadsClient()
	mock.beads["proj-done"] = types.Bead{
		ID:      "proj-done",
		Project: "proj",
		Type:    "task",
		Status:  types.StatusClosed,
	}

	projects := []types.Project{{Name: "proj", Path: "/projects/proj"}}
	completed := map[string]bool{"proj-done": true}
	var mu sync.RWMutex
	d := NewBeadDiscovery(mock, nil, projects, completed, nil, &mu)

	beads, err := d.findFromBR(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBR failed: %v", err)
	}
	if len(beads) != 0 {
		t.Fatalf("expected closed bead to stay filtered, got %d beads: %+v", len(beads), beads)
	}
	if !completed["proj-done"] {
		t.Fatalf("completed entry for genuinely closed bead was wrongly pruned")
	}
}

// A blocked bead in the completed set is also stale: blocked is not terminal.
func TestFindFromBR_PrunesCompletedEntryForBlockedBead(t *testing.T) {
	mock := newMockBeadsClient()
	mock.beads["proj-blk"] = types.Bead{
		ID:      "proj-blk",
		Project: "proj",
		Type:    "task",
		Status:  types.StatusBlocked,
	}

	projects := []types.Project{{Name: "proj", Path: "/projects/proj"}}
	completed := map[string]bool{"proj-blk": true}
	var mu sync.RWMutex
	d := NewBeadDiscovery(mock, nil, projects, completed, nil, &mu)

	beads, err := d.findFromBR(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBR failed: %v", err)
	}
	if len(beads) != 1 || beads[0].ID != "proj-blk" {
		t.Fatalf("expected blocked bead to be schedulable again, got %d beads: %+v", len(beads), beads)
	}
	if completed["proj-blk"] {
		t.Fatalf("stale completed entry for blocked bead was not pruned")
	}
}
