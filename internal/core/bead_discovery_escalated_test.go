package core

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/bv"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Repro for kgo (2026-09-11): hydra-wdp carried the durable "escalated" label
// in issues.jsonl yet was still scheduled — BeadDiscovery's br/bv intake paths
// had no escalated-label gate, unlike dag_navigator.GetReadyBeadsFromDAG.
// The gate lives in filterByDAG, the chokepoint all three paths return through,
// so each findFrom* must exclude escalated beads even with no DAG loaded.
func TestDiscovery_ExcludesEscalatedLabeledBeads(t *testing.T) {
	mock := newMockBeadsClient()
	mock.beads["proj-esc"] = types.Bead{
		ID:      "proj-esc",
		Project: "proj",
		Type:    "task",
		Status:  types.StatusInProgress, // escalated beads are parked in_progress
		Labels:  []string{escalatedLabel},
	}
	mock.beads["proj-ok"] = types.Bead{
		ID:      "proj-ok",
		Project: "proj",
		Type:    "task",
		Status:  types.StatusInProgress,
	}

	root := t.TempDir()
	projPath := filepath.Join(root, "proj")
	if err := os.MkdirAll(projPath, 0o755); err != nil {
		t.Fatal(err)
	}

	exec := &mockBVExecutor{
		triageResponses: map[string]*bv.TriageWrapper{
			projPath: {
				GeneratedAt: "2026-09-20T00:00:00Z",
				Triage: bv.TriageResponse{
					Recommendations: []bv.TriageRecommendation{
						{BeadID: "proj-esc", Title: "esc", Type: "task", Priority: 3, Confidence: 0.5, Reason: "r", Labels: []string{escalatedLabel}},
						{BeadID: "proj-ok", Title: "ok", Type: "task", Priority: 3, Confidence: 0.5, Reason: "r"},
					},
					ReadyCount:   2,
					BlockedCount: 0,
				},
			},
		},
		planResponses: map[string]*bv.PlanWrapper{
			projPath: {
				GeneratedAt: "2026-09-20T00:00:00Z",
				Plan: bv.PlanData{
					Tracks: []bv.PlanTrack{
						{
							TrackID: "A",
							Items: []bv.PlanItem{
								{ID: "proj-esc", Title: "esc", Priority: 3, Status: "open"},
								{ID: "proj-ok", Title: "ok", Priority: 3, Status: "open"},
							},
						},
					},
					TotalActionable: 2,
				},
			},
		},
	}
	bvClient := newBVClientWithExecutor(exec)

	projects := []types.Project{{Name: "proj", Path: projPath}}
	var mu sync.RWMutex
	d := NewBeadDiscovery(mock, bvClient, projects, map[string]bool{}, nil, &mu)

	assertNoEscalated := func(t *testing.T, path string, beads []types.Bead) {
		t.Helper()
		for _, b := range beads {
			if hasBeadLabel(b.Labels, escalatedLabel) {
				t.Fatalf("%s returned escalated-labeled bead %s", path, b.ID)
			}
		}
	}

	brBeads, err := d.findFromBR(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBR failed: %v", err)
	}
	assertNoEscalated(t, "findFromBR", brBeads)
	if len(brBeads) != 1 || brBeads[0].ID != "proj-ok" {
		t.Fatalf("expected only proj-ok from findFromBR, got %+v", brBeads)
	}

	bvBeads, err := d.findFromBV(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBV failed: %v", err)
	}
	assertNoEscalated(t, "findFromBV", bvBeads)
	if len(bvBeads) != 1 || bvBeads[0].ID != "proj-ok" {
		t.Fatalf("expected only proj-ok from findFromBV, got %+v", bvBeads)
	}

	planBeads, err := d.findFromBVPlan(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBVPlan failed: %v", err)
	}
	assertNoEscalated(t, "findFromBVPlan", planBeads)
	if len(planBeads) != 1 || planBeads[0].ID != "proj-ok" {
		t.Fatalf("expected only proj-ok from findFromBVPlan, got %+v", planBeads)
	}
}

// The gate must hold even when no DAG is loaded (dag == nil early return).
func TestFilterByDAG_ExcludesEscalatedWithoutDAG(t *testing.T) {
	projects := []types.Project{{Name: "proj", Path: "/projects/proj"}}
	var mu sync.RWMutex
	d := NewBeadDiscovery(newMockBeadsClient(), nil, projects, map[string]bool{}, nil, &mu)

	candidates := []types.Bead{
		{ID: "esc", Labels: []string{escalatedLabel}},
		{ID: "ok"},
	}
	got := d.filterByDAG(candidates)
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("expected only non-escalated bead without DAG, got %+v", got)
	}
}
