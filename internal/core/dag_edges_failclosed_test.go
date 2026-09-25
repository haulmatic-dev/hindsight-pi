package core

import (
	"errors"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestDAGBuilder_EdgeFetcherSuccess: when EdgeFetcher succeeds, dependencies
// come from the fetched map and the per-bead GetBead loop is skipped entirely
// (kills the N+1 br show contention window).
func TestDAGBuilder_EdgeFetcherSuccess(t *testing.T) {
	client := newMockBeadsClient()
	client.beads["proj-a"] = types.Bead{ID: "proj-a", Project: "proj", Status: types.StatusOpen}
	client.beads["proj-b"] = types.Bead{ID: "proj-b", Project: "proj", Status: types.StatusOpen}

	getBeadCalls := 0
	builder := NewDAGBuilder(NewErrorLogger(nil))
	builder.EdgeFetcher = func(projectPath, repoName string) (map[string][]string, error) {
		return map[string][]string{"proj-b": {"proj-a"}}, nil
	}

	// Wrap GetBead to count calls via getErr sentinel: any call fails loudly.
	client.getErr = errors.New("GetBead must not be called when EdgeFetcher succeeds")

	dag := NewDAG()
	if err := builder.Build(dag, client, []types.Project{{Name: "proj", Path: "proj", Type: types.RepoTypeSingle}}, map[string]bool{}); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_ = getBeadCalls

	nodeB, ok := dag.GetNode("proj-b")
	if !ok {
		t.Fatal("proj-b node missing")
	}
	if nodeB.InDegree != 1 {
		t.Fatalf("proj-b InDegree = %d, want 1 (blocked by proj-a)", nodeB.InDegree)
	}
	ready := dag.GetReadyNodes()
	if len(ready) != 1 || ready[0].Bead.ID != "proj-a" {
		ids := []string{}
		for _, n := range ready {
			ids = append(ids, string(n.Bead.ID))
		}
		t.Fatalf("ready = %v, want [proj-a]", ids)
	}
}

// TestDAGBuilder_GetBeadFailureMarksDepsUnknown: when EdgeFetcher is absent
// and GetBead fails, the bead must be marked deps_unknown (fail-closed) —
// never silently scheduled with an empty dependency set (2026-09-25: br show
// exit-8 under contention dropped 43 gate edges and the hold gate stopped
// binding).
func TestDAGBuilder_GetBeadFailureMarksDepsUnknown(t *testing.T) {
	client := newMockBeadsClient()
	client.beads["proj-a"] = types.Bead{ID: "proj-a", Project: "proj", Status: types.StatusOpen}
	client.getErr = errors.New("br show failed: exit status 8")

	builder := NewDAGBuilder(NewErrorLogger(nil))
	dag := NewDAG()
	if err := builder.Build(dag, client, []types.Project{{Name: "proj", Path: "proj", Type: types.RepoTypeSingle}}, map[string]bool{}); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	node, ok := dag.GetNode("proj-a")
	if !ok {
		t.Fatal("proj-a node missing")
	}
	if node.Bead.Metadata[depsUnknownMarker] != "true" {
		t.Fatalf("deps_unknown marker = %q, want true", node.Bead.Metadata[depsUnknownMarker])
	}
	if ready := dag.GetReadyNodes(); len(ready) != 0 {
		t.Fatalf("ready = %d, want 0 (deps-unknown bead must be held back)", len(ready))
	}
}

// TestDAGBuilder_EdgeFetcherFailureFallsBack: EdgeFetcher error falls back to
// per-bead GetBead, which supplies edges when it succeeds.
func TestDAGBuilder_EdgeFetcherFailureFallsBack(t *testing.T) {
	client := newMockBeadsClient()
	client.beads["proj-a"] = types.Bead{ID: "proj-a", Project: "proj", Status: types.StatusOpen}
	client.beads["proj-b"] = types.Bead{ID: "proj-b", Project: "proj", Status: types.StatusOpen,
		Dependencies: []types.BeadID{"proj-a"}}

	builder := NewDAGBuilder(NewErrorLogger(nil))
	builder.EdgeFetcher = func(projectPath, repoName string) (map[string][]string, error) {
		return nil, errors.New("bv unavailable")
	}

	dag := NewDAG()
	if err := builder.Build(dag, client, []types.Project{{Name: "proj", Path: "proj", Type: types.RepoTypeSingle}}, map[string]bool{}); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if nodeB, ok := dag.GetNode("proj-b"); !ok || nodeB.InDegree != 1 {
		t.Fatalf("proj-b InDegree = %v, want 1 via GetBead fallback", nodeB)
	}
}

// TestIsHoldBead: HOLD gates must never schedule as work.
func TestIsHoldBead(t *testing.T) {
	cases := []struct {
		name string
		bead types.Bead
		want bool
	}{
		{"title prefix", types.Bead{Title: "HOLD: campaign paused"}, true},
		{"title prefix lowercase", types.Bead{Title: "hold: lowercase"}, true},
		{"type hold", types.Bead{Type: "hold", Title: "Pause"}, true},
		{"label hold", types.Bead{Labels: []string{"hold"}, Title: "Pause"}, true},
		{"normal bead", types.Bead{Title: "RED: Write tests", Type: "test"}, false},
		{"title contains hold", types.Bead{Title: "Implement holdout validation"}, false},
	}
	for _, tc := range cases {
		if got := isHoldBead(tc.bead); got != tc.want {
			t.Errorf("%s: isHoldBead = %v, want %v", tc.name, got, tc.want)
		}
	}
}
