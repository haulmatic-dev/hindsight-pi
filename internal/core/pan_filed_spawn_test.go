package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// hydra-bldo: pan-filed beads are human-triage filings (Pan file-bead
// actuator). A worker must never execute them. Two layers:
//  1. discovery gate — filterByDAG drops pan-filed candidates
//  2. spawn preflight — spawnPreflightBead blocks with suppression

func TestFilterByDAG_ExcludesPanFiled(t *testing.T) {
	d := NewBeadDiscovery(newMockBeadsClient(), nil,
		[]types.Project{{Name: "proj", Path: "/projects/proj"}}, map[string]bool{}, nil, nil)
	in := []types.Bead{
		{ID: "proj-filed1", Project: "proj", Type: "task", Status: types.StatusOpen,
			Labels: []string{panFiledLabel}},
		{ID: "proj-work1", Project: "proj", Type: "task", Status: types.StatusOpen},
		{ID: "proj-filed2", Project: "proj", Type: "task", Status: types.StatusOpen,
			Labels: []string{"pan-filed", "needs-human"}},
	}
	out := d.filterByDAG(in)
	for _, b := range out {
		if hasBeadLabel(b.Labels, panFiledLabel) {
			t.Fatalf("filterByDAG returned pan-filed bead %s", b.ID)
		}
	}
	if len(out) != 1 || out[0].ID != "proj-work1" {
		t.Fatalf("expected only proj-work1, got %+v", out)
	}
}

func TestFilterByDAG_NoDAG_PanFiledStillFiltered(t *testing.T) {
	// dag == nil early return must not bypass the pan-filed gate.
	d := NewBeadDiscovery(newMockBeadsClient(), nil,
		[]types.Project{{Name: "proj", Path: "/projects/proj"}}, map[string]bool{}, nil, nil)
	out := d.filterByDAG([]types.Bead{
		{ID: "proj-filed3", Project: "proj", Type: "task", Labels: []string{panFiledLabel}},
	})
	if len(out) != 0 {
		t.Fatalf("expected pan-filed bead filtered with nil DAG, got %+v", out)
	}
}

func TestSpawnPreflight_PanFiledLabelBlocked(t *testing.T) {
	o := mockOverseerForPreflight(t)
	bead := types.Bead{ID: "proj-fld9", Project: "proj", Type: "task", Status: types.StatusOpen,
		Title: "campaign anomaly", Labels: []string{panFiledLabel}}
	err := o.spawnPreflightBead(bead, "/tmp/proj")
	if err == nil {
		t.Fatal("pan-filed bead should be blocked at spawn funnel")
	}
	var sup *SpawnSuppressedError
	if !errors.As(err, &sup) {
		t.Fatalf("expected SpawnSuppressedError, got %T: %v", err, sup)
	}
	if !strings.Contains(sup.Error(), "pan-filed") {
		t.Errorf("suppression should mention pan-filed, got: %s", sup.Error())
	}
}

func TestSpawnWorker_PanFiledBlocked(t *testing.T) {
	// End-to-end through the spawn funnel: store truth carries the pan-filed
	// label; the worker spawn must be suppressed (no retry burned).
	o := mockOverseerForPreflight(t)
	mock := o.beadsClient.(*mockBeadsClient)
	mock.beads["proj-fldA"] = types.Bead{ID: "proj-fldA", Project: "proj",
		Type: "task", Status: types.StatusOpen, Title: "triage", Labels: []string{panFiledLabel}}
	err := o.spawnWorker(types.Bead{ID: "proj-fldA", Project: "proj", Type: "task", Status: types.StatusOpen, Title: "triage"})
	if err == nil {
		t.Fatal("expected pan-filed spawn to be suppressed")
	}
	if _, ok := err.(*SpawnSuppressedError); !ok {
		t.Fatalf("expected *SpawnSuppressedError, got %T: %v", err, err)
	}
}

func TestSpawnPreflight_NonPanFiledLabelAllowed(t *testing.T) {
	o := mockOverseerForPreflight(t)
	bead := types.Bead{ID: "proj-ok9", Project: "proj", Type: "task", Status: types.StatusOpen,
		Title: "real work", Labels: []string{"needs-human"}}
	if err := o.spawnPreflightBead(bead, "/tmp/proj"); err != nil {
		t.Fatalf("non-pan-filed bead should pass preflight, got: %v", err)
	}
}
