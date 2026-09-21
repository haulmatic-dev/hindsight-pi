package core

import (
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Two projects with the same repo name (ajantha's webapp + nipuna's
// webapp): the ID prefix alone cannot disambiguate — the index must return
// whichever store actually listed the bead.
func TestProvenanceIndexDisambiguatesSameRepoName(t *testing.T) {
	idx := &BeadProvenanceIndex{}
	ajanthaPath := "/home/ajantha/projects/webapp"
	nipunaPath := "/home/nipuna.j/projects/webapp"

	idx.Note(ajanthaPath, []string{"webapp-abc", "webapp-def"})
	idx.Note(nipunaPath, []string{"webapp-xyz"})

	if got := idx.Lookup("webapp-xyz"); got != nipunaPath {
		t.Errorf("Lookup(webapp-xyz) = %q, want nipuna's path %q", got, nipunaPath)
	}
	if got := idx.Lookup("webapp-abc"); got != ajanthaPath {
		t.Errorf("Lookup(webapp-abc) = %q, want ajantha's path %q", got, ajanthaPath)
	}
	if got := idx.Lookup("never-listed"); got != "" {
		t.Errorf("Lookup(never-listed) = %q, want empty", got)
	}
}

// Reset drops stale provenance between DAG rebuilds (removed beads must not
// resolve forever).
func TestProvenanceIndexReset(t *testing.T) {
	idx := &BeadProvenanceIndex{}
	idx.Note("/p1", []string{"webapp-abc"})
	idx.Reset()
	if got := idx.Lookup("webapp-abc"); got != "" {
		t.Errorf("after Reset Lookup = %q, want empty", got)
	}
	idx.Note("/p2", []string{"webapp-abc"})
	if got := idx.Lookup("webapp-abc"); got != "/p2" {
		t.Errorf("after re-note Lookup = %q, want /p2", got)
	}
}

// Nil index is safe everywhere (unwired paths in tests).
func TestProvenanceIndexNilSafe(t *testing.T) {
	var idx *BeadProvenanceIndex
	idx.Note("/p", []string{"a"})
	if got := idx.Lookup("a"); got != "" {
		t.Errorf("nil Lookup = %q, want empty", got)
	}
	idx.Reset()
}

// Regression: two projects with the same repo name — resolution must follow
// the store that listed the bead, not the first exact-name config match.
func TestFindProjectPathProvenanceBeatsNameCollision(t *testing.T) {
	ajanthaPath := "/home/ajantha/projects/webapp"
	nipunaPath := "/home/nipuna.j/projects/webapp"
	projects := projectsWithSameRepoName(ajanthaPath, nipunaPath)

	idx := &BeadProvenanceIndex{}
	idx.Note(nipunaPath, []string{"webapp-xyz"})

	ba := NewBeadAnalyzer(projects)
	ba.SetProvenanceIndex(idx)

	bead := &types.Bead{ID: "webapp-xyz"}
	if got := ba.FindProjectPath(bead); got != nipunaPath {
		t.Errorf("FindProjectPath = %q, want nipuna's store %q (provenance must beat name collision)", got, nipunaPath)
	}

	// Without provenance, the first exact-name match hijacks the bead.
	bare := NewBeadAnalyzer(projects)
	if got := bare.FindProjectPath(bead); got != ajanthaPath {
		t.Errorf("bare FindProjectPath = %q, want %q (documents the old hijack order)", got, ajanthaPath)
	}
}

func projectsWithSameRepoName(ajanthaPath, nipunaPath string) []types.Project {
	return []types.Project{
		{Name: "webapp", Path: ajanthaPath, Type: types.RepoTypeSingle},
		{Name: "webapp", Path: nipunaPath, Type: types.RepoTypeSingle},
	}
}

// Regression: discovered multi-repo projects stored a bare relative repo name
// in provenance, so spawn resolved a relative path and git walked up to the
// daemon's monorepo (srv-telematics incident, 2026-09-21). The DAG builder
// must record the absolute per-repo path for every listed bead.
func TestDAGBuilderRecordsAbsoluteRepoPath(t *testing.T) {
	parent := "/home/buddhi/projects-hydra-repro"
	repoName := "tiny-greeter"
	expectedRepoPath := filepath.Join(parent, repoName)

	project := types.Project{
		Name: "projects-hydra-repro",
		Path: parent,
		Type: types.RepoTypeMulti,
		Repos: []types.Repo{
			{Name: repoName, Path: repoName}, // discovery stores the bare name
		},
	}

	beadID := types.BeadID("projects-hydra-repro/" + repoName + "-u1i")
	bead := types.Bead{
		ID:       beadID,
		Project:  project.Name,
		Repo:     repoName,
		Status:   types.StatusOpen,
	}

	client := newMockBeadsClient()
	client.beads[string(beadID)] = bead

	provenance := &BeadProvenanceIndex{}
	builder := NewDAGBuilder(NewErrorLogger(nil))
	builder.Provenance = provenance

	if err := builder.Build(NewDAG(), client, []types.Project{project}, nil); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := provenance.Lookup(string(beadID)); got != expectedRepoPath {
		t.Errorf("provenance path = %q, want absolute repo path %q", got, expectedRepoPath)
	}
}
