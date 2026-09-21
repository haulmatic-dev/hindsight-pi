package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/internal/bv"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// mockBVExecutor fakes bv --robot-triage / --robot-plan responses per repo
// directory so we can test multi-repo attribution without invoking the real
// bv binary.
type mockBVExecutor struct {
	triageResponses map[string]*bv.TriageWrapper
	planResponses   map[string]*bv.PlanWrapper
}

func (m *mockBVExecutor) Execute(name string, arg ...string) ([]byte, error) {
	if name == "bv" && len(arg) > 0 && arg[0] == "-version" {
		return []byte("v0.0.0-test"), nil
	}
	return nil, fmt.Errorf("unexpected command: %s %v", name, arg)
}

func (m *mockBVExecutor) ExecuteInDir(dir string, name string, arg ...string) ([]byte, error) {
	if name != "bv" {
		return nil, fmt.Errorf("unexpected command: %s", name)
	}
	if len(arg) == 0 {
		return nil, fmt.Errorf("no args")
	}
	switch arg[0] {
	case "--robot-triage":
		w, ok := m.triageResponses[dir]
		if !ok {
			return nil, fmt.Errorf("no triage response for %s", dir)
		}
		return json.Marshal(w)
	case "--robot-plan":
		w, ok := m.planResponses[dir]
		if !ok {
			return nil, fmt.Errorf("no plan response for %s", dir)
		}
		return json.Marshal(w)
	default:
		return nil, fmt.Errorf("unexpected bv flag: %s", arg[0])
	}
}

// repoAwareMockBeadsClient distinguishes beads by repo so that ID collisions
// across repos can be modelled correctly.
type repoAwareMockBeadsClient struct {
	mu    sync.RWMutex
	beads map[string]*types.Bead // key: repoName:beadID
}

func newRepoAwareMockBeadsClient() *repoAwareMockBeadsClient {
	return &repoAwareMockBeadsClient{beads: make(map[string]*types.Bead)}
}

func (m *repoAwareMockBeadsClient) add(repoName string, bead types.Bead) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beads[repoName+":"+string(bead.ID)] = &bead
}

func (m *repoAwareMockBeadsClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if b, ok := m.beads[repoName+":"+string(beadID)]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("bead %s not found in repo %s", beadID, repoName)
}

func (m *repoAwareMockBeadsClient) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}
func (m *repoAwareMockBeadsClient) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}
func (m *repoAwareMockBeadsClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	return nil
}
func (m *repoAwareMockBeadsClient) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	return nil
}
func (m *repoAwareMockBeadsClient) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	return "", nil
}
func (m *repoAwareMockBeadsClient) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}
func (m *repoAwareMockBeadsClient) RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}
func (m *repoAwareMockBeadsClient) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	return nil
}

func (m *repoAwareMockBeadsClient) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	return nil
}
func (m *repoAwareMockBeadsClient) AddDependency(projectPath, repoName string, beadID, dependsOn types.BeadID) error {
	return nil
}

func newBVClientWithExecutor(exec *mockBVExecutor) *bv.Client {
	c := bv.NewClient(bv.WithExecutor(exec))
	// Warm the health-check cache so discovery does not bypass the client.
	if !c.IsHealthy() {
		panic("mock bv client not healthy")
	}
	return c
}

func TestFindFromBV_MultiRepoAttribution(t *testing.T) {
	root := t.TempDir()
	alphaPath := filepath.Join(root, "alpha")
	betaPath := filepath.Join(root, "beta")
	_ = os.MkdirAll(alphaPath, 0o755)
	_ = os.MkdirAll(betaPath, 0o755)

	exec := &mockBVExecutor{
		triageResponses: map[string]*bv.TriageWrapper{
			alphaPath: {
				GeneratedAt: "2026-09-16T00:00:00Z",
				DataHash:    "alpha",
				Triage: bv.TriageResponse{
					Recommendations: []bv.TriageRecommendation{
						{BeadID: "alpha-ib3", Title: "Alpha ready", Type: "task", Priority: 3, Confidence: 0.5, Reason: "r"},
					},
					ReadyCount:   1,
					BlockedCount: 0,
				},
			},
			betaPath: {
				GeneratedAt: "2026-09-16T00:00:00Z",
				DataHash:    "beta",
				Triage: bv.TriageResponse{
					Recommendations: []bv.TriageRecommendation{
						{BeadID: "beta-lxb", Title: "Beta ready", Type: "task", Priority: 3, Confidence: 0.5, Reason: "r"},
					},
					ReadyCount:   1,
					BlockedCount: 0,
				},
			},
		},
	}

	beads := newRepoAwareMockBeadsClient()
	beads.add("alpha", types.Bead{ID: "alpha-ib3", Project: "multi", Repo: "alpha", Status: types.StatusOpen})
	beads.add("beta", types.Bead{ID: "beta-lxb", Project: "multi", Repo: "beta", Status: types.StatusOpen})

	project := types.Project{
		Name:  "multi",
		Path:  root,
		Type:  types.RepoTypeMulti,
		Repos: []types.Repo{{Name: "alpha", Path: "alpha"}, {Name: "beta", Path: "beta"}},
	}

	d := NewBeadDiscovery(beads, newBVClientWithExecutor(exec), []types.Project{project}, map[string]bool{}, nil, &sync.RWMutex{})
	ready, err := d.findFromBV(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBV failed: %v", err)
	}
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready beads, got %d: %+v", len(ready), ready)
	}

	got := map[string]string{}
	for _, b := range ready {
		got[string(b.ID)] = b.Repo
	}
	if got["alpha-ib3"] != "alpha" {
		t.Errorf("alpha-ib3 repo = %q, want alpha", got["alpha-ib3"])
	}
	if got["beta-lxb"] != "beta" {
		t.Errorf("beta-lxb repo = %q, want beta", got["beta-lxb"])
	}
}

func TestFindFromBV_CollisionDoesNotSuppressCrossRepo(t *testing.T) {
	root := t.TempDir()
	alphaPath := filepath.Join(root, "alpha")
	betaPath := filepath.Join(root, "beta")
	_ = os.MkdirAll(alphaPath, 0o755)
	_ = os.MkdirAll(betaPath, 0o755)

	// Same bead ID exists in both repos. Beta's copy is closed; alpha's is open.
	// Per-repo bv invocation must not let the closed beta copy suppress alpha.
	exec := &mockBVExecutor{
		triageResponses: map[string]*bv.TriageWrapper{
			alphaPath: {
				GeneratedAt: "2026-09-16T00:00:00Z",
				DataHash:    "alpha",
				Triage: bv.TriageResponse{
					Recommendations: []bv.TriageRecommendation{
						{BeadID: "shared-1", Title: "Alpha shared", Type: "task", Priority: 3, Confidence: 0.5, Reason: "r"},
					},
					ReadyCount:   1,
					BlockedCount: 0,
				},
			},
			betaPath: {
				GeneratedAt: "2026-09-16T00:00:00Z",
				DataHash:    "beta",
				Triage: bv.TriageResponse{
					Recommendations: []bv.TriageRecommendation{
						{BeadID: "shared-1", Title: "Beta shared", Type: "task", Priority: 3, Confidence: 0.5, Reason: "r"},
					},
					ReadyCount:   1,
					BlockedCount: 0,
				},
			},
		},
	}

	beads := newRepoAwareMockBeadsClient()
	beads.add("alpha", types.Bead{ID: "shared-1", Project: "multi", Repo: "alpha", Status: types.StatusOpen})
	beads.add("beta", types.Bead{ID: "shared-1", Project: "multi", Repo: "beta", Status: types.StatusClosed})

	project := types.Project{
		Name:  "multi",
		Path:  root,
		Type:  types.RepoTypeMulti,
		Repos: []types.Repo{{Name: "alpha", Path: "alpha"}, {Name: "beta", Path: "beta"}},
	}

	completed := map[string]bool{"shared-1": true}
	d := NewBeadDiscovery(beads, newBVClientWithExecutor(exec), []types.Project{project}, completed, nil, &sync.RWMutex{})
	ready, err := d.findFromBV(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBV failed: %v", err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready bead (alpha's open copy), got %d: %+v", len(ready), ready)
	}
	if string(ready[0].ID) != "shared-1" || ready[0].Repo != "alpha" {
		t.Errorf("got %+v; want shared-1 from alpha", ready[0])
	}
}

func TestFindFromBVPlan_MultiRepoAttribution(t *testing.T) {
	root := t.TempDir()
	alphaPath := filepath.Join(root, "alpha")
	betaPath := filepath.Join(root, "beta")
	_ = os.MkdirAll(alphaPath, 0o755)
	_ = os.MkdirAll(betaPath, 0o755)

	exec := &mockBVExecutor{
		planResponses: map[string]*bv.PlanWrapper{
			alphaPath: {
				GeneratedAt: "2026-09-16T00:00:00Z",
				DataHash:    "alpha",
				Plan: bv.PlanData{
					Tracks: []bv.PlanTrack{
						{
							TrackID: "A",
							Items: []bv.PlanItem{
								{ID: "alpha-ib3", Title: "Alpha ready", Priority: 3, Status: "open"},
							},
						},
					},
					TotalActionable: 1,
				},
			},
			betaPath: {
				GeneratedAt: "2026-09-16T00:00:00Z",
				DataHash:    "beta",
				Plan: bv.PlanData{
					Tracks: []bv.PlanTrack{
						{
							TrackID: "B",
							Items: []bv.PlanItem{
								{ID: "beta-lxb", Title: "Beta ready", Priority: 3, Status: "open"},
							},
						},
					},
					TotalActionable: 1,
				},
			},
		},
	}

	beads := newRepoAwareMockBeadsClient()
	beads.add("alpha", types.Bead{ID: "alpha-ib3", Project: "multi", Repo: "alpha", Status: types.StatusOpen})
	beads.add("beta", types.Bead{ID: "beta-lxb", Project: "multi", Repo: "beta", Status: types.StatusOpen})

	project := types.Project{
		Name:  "multi",
		Path:  root,
		Type:  types.RepoTypeMulti,
		Repos: []types.Repo{{Name: "alpha", Path: "alpha"}, {Name: "beta", Path: "beta"}},
	}

	d := NewBeadDiscovery(beads, newBVClientWithExecutor(exec), []types.Project{project}, map[string]bool{}, nil, &sync.RWMutex{})
	ready, err := d.findFromBVPlan(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBVPlan failed: %v", err)
	}
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready beads, got %d: %+v", len(ready), ready)
	}
	if string(ready[0].ID) == "alpha-ib3" && ready[0].Repo != "alpha" {
		t.Errorf("alpha-ib3 repo = %q, want alpha", ready[0].Repo)
	}
	if string(ready[1].ID) == "beta-lxb" && ready[1].Repo != "beta" {
		t.Errorf("beta-lxb repo = %q, want beta", ready[1].Repo)
	}
}

func TestResolveRepoName(t *testing.T) {
	d := &BeadDiscovery{}
	logged := map[string]bool{}

	project := types.Project{
		Name:  "multi",
		Path:  "/projects/multi",
		Type:  types.RepoTypeMulti,
		Repos: []types.Repo{{Name: "alpha", Path: "alpha"}, {Name: "beta", Path: "beta"}},
	}

	// bv emits explicit repo field: use it.
	if got := d.resolveRepoName(project, "/projects/multi/alpha", "override", logged); got != "override" {
		t.Errorf("explicit repo field ignored: got %q", got)
	}

	// No repo field, multi with >1 repos: derive from target.
	if got := d.resolveRepoName(project, "/projects/multi/beta", "", logged); got != "beta" {
		t.Errorf("target-derived repo = %q, want beta", got)
	}

	// Single-repo project: empty repo.
	single := types.Project{Name: "single", Path: "/projects/single", Type: types.RepoTypeSingle}
	if got := d.resolveRepoName(single, "/projects/single", "", logged); got != "" {
		t.Errorf("single-repo repo = %q, want empty", got)
	}

	// Multi with exactly one repo: fallback to that repo.
	oneRepo := types.Project{
		Name:  "multi",
		Path:  "/projects/multi",
		Type:  types.RepoTypeMulti,
		Repos: []types.Repo{{Name: "only", Path: "only"}},
	}
	if got := d.resolveRepoName(oneRepo, "/projects/multi", "", logged); got != "only" {
		t.Errorf("one-repo fallback = %q, want only", got)
	}
}

// TestBeadDiscovery_RealMultiRepo drives the real bv/br stack against a
// scratch multi-repo directory. It verifies that beads discovered via bv are
// attributed to their source repo (not Repos[0]).
func TestBeadDiscovery_RealMultiRepo(t *testing.T) {
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br binary not available:", err)
	}
	bvPath, err := exec.LookPath("bv")
	if err != nil {
		t.Skip("bv binary not available:", err)
	}
	_ = bvPath

	root := t.TempDir()
	alphaPath := filepath.Join(root, "alpha")
	betaPath := filepath.Join(root, "beta")
	mustRun(t, "git", "init", "-q", alphaPath)
	mustRun(t, "git", "init", "-q", betaPath)
	mustRunIn(t, alphaPath, "br", "init")
	mustRunIn(t, betaPath, "br", "init")

	// alpha: one ready, one blocked (depends on ready)
	alphaReady := mustCreate(t, brPath, alphaPath, "Alpha ready", "task", 3)
	alphaBlocked := mustCreate(t, brPath, alphaPath, "Alpha blocked", "task", 3)
	mustRunIn(t, alphaPath, "br", "dep", "add", alphaBlocked, alphaReady)

	// beta: one ready, one closed
	betaReady := mustCreate(t, brPath, betaPath, "Beta ready", "task", 3)
	betaClosed := mustCreate(t, brPath, betaPath, "Beta closed", "task", 3)
	mustRunIn(t, betaPath, "br", "close", betaClosed, "-r", "done")

	// Build the project exactly as multi-user discovery would.
	project := types.Project{
		Name:  "scratch",
		Path:  root,
		Type:  types.RepoTypeMulti,
		Repos: []types.Repo{{Name: "alpha", Path: "alpha"}, {Name: "beta", Path: "beta"}},
	}

	beadsClient := beads.NewClient()
	bvClient := bv.NewClient()
	if !bvClient.IsHealthy() {
		t.Skip("bv not healthy")
	}

	d := NewBeadDiscovery(beadsClient, bvClient, []types.Project{project}, map[string]bool{}, nil, &sync.RWMutex{})
	ready, err := d.findFromBVPlan(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBVPlan failed: %v", err)
	}

	byRepo := map[string][]string{}
	for _, b := range ready {
		byRepo[b.Repo] = append(byRepo[b.Repo], string(b.ID))
	}

	if len(byRepo["alpha"]) == 0 {
		t.Errorf("no alpha beads attributed to alpha repo")
	}
	if len(byRepo["beta"]) == 0 {
		t.Errorf("no beta beads attributed to beta repo")
	}
	betaFound := map[string]bool{}
	for _, id := range byRepo["beta"] {
		betaFound[id] = true
		if strings.Contains(id, betaClosed) {
			t.Errorf("closed beta bead %s was scheduled", betaClosed)
		}
	}
	if !betaFound[betaReady] {
		t.Errorf("beta ready bead %s not attributed to beta repo", betaReady)
	}

	// Verify the triage path too.
	ready2, err := d.findFromBV(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBV failed: %v", err)
	}
	for _, b := range ready2 {
		if b.Repo != "alpha" && b.Repo != "beta" {
			t.Errorf("bead %s has unexpected repo %q", b.ID, b.Repo)
		}
	}
}

// TestRefreshDiscoveredProjects_RealReposMidRun verifies the dynamic-scan
// contract against real br-initialized repos: a repo dropped into the multi
// dir mid-run is picked up by refreshDiscoveredProjects without a restart.
func TestRefreshDiscoveredProjects_RealReposMidRun(t *testing.T) {
	if _, err := exec.LookPath("br"); err != nil {
		t.Skip("br binary not available:", err)
	}
	home := t.TempDir()
	writeDiscoveryUserConfig(t, home, "u", `
projects:
  - name: multi
    path: ~/projects/multi
    type: multi
`)

	initRepo := func(name string) {
		dir := filepath.Join(home, "u", "projects", "multi", name)
		mustRun(t, "git", "init", "-q", dir)
		mustRunIn(t, dir, "br", "init")
	}
	initRepo("alpha")

	o := &Overseer{
		config:           DefaultConfig(),
		discoveryHomeDir: home,
	}
	o.config.MultiUserDiscovery = true
	o.beadAnalyzer = NewBeadAnalyzer(nil)
	o.beadFileReader = NewBeadFileReader(nil)

	o.refreshDiscoveredProjects()
	if len(o.projects) != 1 || len(o.projects[0].Repos) != 1 || o.projects[0].Repos[0].Name != "alpha" {
		t.Fatalf("after first refresh: %+v", o.projects)
	}

	// New repo appears mid-run.
	initRepo("beta")
	o.refreshDiscoveredProjects()
	if len(o.projects) != 1 {
		t.Fatalf("projects after second refresh = %d, want 1", len(o.projects))
	}
	if got := len(o.projects[0].Repos); got != 2 {
		t.Fatalf("repos after second refresh = %d, want 2 (alpha+beta): %+v", got, o.projects[0].Repos)
	}
	names := map[string]bool{}
	for _, r := range o.projects[0].Repos {
		names[r.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("repos = %+v, want alpha and beta", o.projects[0].Repos)
	}
}

func mustRun(t *testing.T, name string, arg ...string) {
	t.Helper()
	cmd := exec.Command(name, arg...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, arg, err, out)
	}
}

// mustRunIn runs a command with cmd.Dir set to dir.
func mustRunIn(t *testing.T, dir, name string, arg ...string) {
	t.Helper()
	cmd := exec.Command(name, arg...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s failed: %v\n%s", name, arg, dir, err, out)
	}
}

func mustCreate(t *testing.T, brPath, repoPath, title, typ string, priority int) string {
	t.Helper()
	cmd := exec.Command(brPath, "create", title, "--type", typ, "--priority", fmt.Sprintf("%d", priority), "--silent")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("br create failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}
