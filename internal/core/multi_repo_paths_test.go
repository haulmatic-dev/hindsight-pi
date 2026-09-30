package core

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// fakeMultiListClient routes ListBeads per directory: only the given repoDir
// returns beads; the parent dir (multi project path) errors, mirroring a
// real br invocation in a directory without a .beads store.
type fakeMultiListClient struct {
	mu      sync.Mutex
	repoDir string
	list    []types.Bead
	closed  []string
}

func (f *fakeMultiListClient) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	if projectPath == f.repoDir {
		return f.list, nil
	}
	return nil, errors.New("br list failed exit status 2")
}
func (f *fakeMultiListClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	if projectPath == f.repoDir {
		for i := range f.list {
			if string(f.list[i].ID) == string(beadID) {
				return &f.list[i], nil
			}
		}
	}
	return nil, nil
}
func (f *fakeMultiListClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, projectPath+":"+string(beadID))
	return nil
}
func (f *fakeMultiListClient) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}
func (f *fakeMultiListClient) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	return nil
}
func (f *fakeMultiListClient) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	return "", nil
}
func (f *fakeMultiListClient) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}
func (f *fakeMultiListClient) RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}
func (f *fakeMultiListClient) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	return nil
}
func (f *fakeMultiListClient) AddDependency(projectPath, repoName string, beadID, dependsOn types.BeadID) error {
	return nil
}
func (f *fakeMultiListClient) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	return nil
}

// TestIntegrationRetest_MultiRepoUsesRepoPath: a multi project's parked beads
// are listed per repo dir, not from the parent (which has no .beads store).
func TestIntegrationRetest_MultiRepoUsesRepoPath(t *testing.T) {
	s := retestTestStage()
	repoDir := filepath.Join(t.TempDir(), "fms-frontend")
	fe := &fakeMultiListClient{repoDir: repoDir}
	s.beadsClient = fe

	multi := types.Project{
		Name: "fms",
		Path: filepath.Dir(repoDir),
		Type: types.RepoTypeMulti,
		Repos: []types.Repo{
			{Name: "fms-frontend", Path: "fms-frontend"},
		},
	}
	// selectBead needs a selectable bead; findBranch on a non-git dir
	// returns "" via the gitFn error path, so runBead exits early without
	// touching anything — but ListBeads must have hit the repo dir.
	fe.list = []types.Bead{{ID: "fmsfe-abc", Status: types.StatusOpen, Labels: []string{retestLabelExhausted}}}
	s.runProject(multi, nil)
	// No panic, no parent-dir br call failure: ListBeads succeeded (the
	// fake errors for the parent). Assert via the fastpath: the sweep
	// selected the bead (allowAttempt state keyed by repoPath exists).
	s.mu.Lock()
	_, keyed := s.lastAttempt["fmsfe-abc"]
	s.mu.Unlock()
	if !keyed {
		t.Fatalf("runProject must select beads via the repo store %s, not the parent", repoDir)
	}
}

// TestFindProjectPathForBead_MultiRepo: epic persistence resolves the repo
// dir that owns the bead, not the parent path.
func TestFindProjectPathForBead_MultiRepo(t *testing.T) {
	o := &Overseer{}
	repoDir := filepath.Join(t.TempDir(), "fms-frontend")
	o.projects = []types.Project{{
		Name: "fms",
		Path: filepath.Dir(repoDir),
		Type: types.RepoTypeMulti,
		Repos: []types.Repo{
			{Name: "fms-frontend", Path: "fms-frontend"},
		},
	}}
	o.beadsClient = &fakeMultiListClient{repoDir: repoDir, list: []types.Bead{{ID: "fmsfe-b9o"}}}

	path, _ := o.findProjectPathForBead("fmsfe-b9o")
	if path != repoDir {
		t.Fatalf("findProjectPathForBead = %q, want repo dir %q", path, repoDir)
	}
	// Unknown ID stays empty (no prefix match, no store hit) — fail-open,
	path, _ = o.findProjectPathForBead("fmsfe-zzzz")
	if path != "" {
		t.Fatalf("unknown bead fallback = %q, want parent %q", path, filepath.Dir(repoDir))
	}
}

// TestRepoDirForBead_SingleProject: single projects return project.Path
// directly without any scan.
func TestRepoDirForBead_SingleProject(t *testing.T) {
	p := types.Project{Name: "solo", Path: "/tmp/solo", Type: types.RepoTypeSingle}
	if got := repoDirForBead(p, "solo-abc", nil); got != "/tmp/solo" {
		t.Fatalf("repoDirForBead single = %q", got)
	}
}
