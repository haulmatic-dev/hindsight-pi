package core

// Tests for the integration-retest stage: selector, rate-limit, merge-tree
// parsing (pure), gate orchestration with fakes, and an end-to-end sweep
// over two temp git repos (clean case lands via the dogfood path, conflicted
// case stays parked).

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestParseMergeTreeResult(t *testing.T) {
	cases := []struct {
		name    string
		exit    int
		out     string
		runErr  error
		wantOK  bool
		wantErr bool
	}{
		{name: "clean", exit: 0, out: "4b825dc642cb6eb9a060e54bf8d69288fbee4904\n", wantOK: true},
		{name: "conflict", exit: 1, out: "CONFLICT (content): Merge conflict in main.go\n", wantOK: false},
		{name: "exit1-no-markers", exit: 1, out: "something odd\n", wantErr: true},
		{name: "fatal", exit: 128, out: "fatal: not a repository\n", wantErr: true},
		{name: "runner-fault", exit: -1, runErr: retestErrFake("spawn git: no such file"), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := parseMergeTreeResult(tc.exit, tc.out, tc.runErr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got ok=%v", ok)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOK)
			}
		})
	}
}

type retestFakeErr string

func (e retestFakeErr) Error() string { return string(e) }

func retestErrFake(s string) error { return retestFakeErr(s) }

func retestTestStage() *IntegrationRetestStage {
	s := NewIntegrationRetestStage(IntegrationRetestConfig{
		Enabled: true, MaxAttempts: 2, RetryInterval: "1h",
	}, &Config{}, nil, nil, nil, "")
	s.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return s
}

func TestSelectBead(t *testing.T) {
	open := types.StatusOpen
	closed := types.StatusClosed
	cases := []struct {
		name      string
		labels    []string
		status    types.BeadStatus
		active    bool
		hasBranch bool
		want      bool
	}{
		{name: "integration-failed", labels: []string{retestLabelFailed}, status: open, hasBranch: true, want: true},
		{name: "integration-exhausted", labels: []string{retestLabelExhausted}, status: open, hasBranch: true, want: true},
		{name: "no-label", labels: []string{"gate-failed"}, status: open, hasBranch: true, want: false},
		{name: "terminal", labels: []string{retestLabelFailed}, status: closed, hasBranch: true, want: false},
		{name: "parked-other-cause", labels: []string{retestLabelExhausted, worktreeUnavailableLabel}, status: open, hasBranch: true, want: false},
		{name: "infra-excluded", labels: []string{retestLabelFailed, "infra-exhausted"}, status: open, hasBranch: true, want: false},
		{name: "active-worker", labels: []string{retestLabelFailed}, status: open, active: true, hasBranch: true, want: false},
		{name: "branch-gone", labels: []string{retestLabelFailed}, status: open, hasBranch: false, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := retestTestStage()
			s.gitFn = func(dir string, args ...string) (string, error) {
				if !tc.hasBranch {
					return "", retestFakeErr("no match")
				}
				return "hydra/wt-proj-abc\n", nil
			}
			bead := &types.Bead{ID: "proj-abc", Status: tc.status, Labels: tc.labels}
			active := map[string]bool{}
			if tc.active {
				active["proj-abc"] = true
			}
			if got := s.selectBead("/proj", bead, active); got != tc.want {
				t.Fatalf("selectBead=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestAllowAttemptRateLimitAndCap(t *testing.T) {
	s := retestTestStage()
	now := time.Unix(1_700_000_000, 0)
	s.now = func() time.Time { return now }

	if !s.allowAttempt("/proj", "b1") {
		t.Fatal("first attempt must be allowed")
	}
	if s.allowAttempt("/proj", "b1") {
		t.Fatal("second attempt inside retry interval must be denied")
	}
	now = now.Add(2 * time.Hour)
	if !s.allowAttempt("/proj", "b1") {
		t.Fatal("attempt after retry interval must be allowed")
	}
	s.countAttempt("b1")
	s.countAttempt("b1") // hits MaxAttempts=2
	now = now.Add(2 * time.Hour)
	if s.allowAttempt("/proj", "b1") {
		t.Fatal("attempt beyond cap must be denied")
	}
}

func TestRunGatesDefaultFailOpen(t *testing.T) {
	s := retestTestStage() // gateRunner nil, no config gates
	dir := t.TempDir()
	if err := s.runDefaultGates("proj", &types.Bead{ID: "proj-abc"}, dir, dir); err != nil {
		t.Fatalf("undetectable stack must fail open, got: %v", err)
	}
}

// fakeRetestBeadsClient records close/label calls.
type fakeRetestBeadsClient struct {
	mu        sync.Mutex
	list      []types.Bead
	closed    []string
	removed   [][]string
	statusErr error
}

func (f *fakeRetestBeadsClient) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	return f.list, nil
}
func (f *fakeRetestBeadsClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	return nil, nil
}
func (f *fakeRetestBeadsClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, string(beadID)+": "+reason)
	return f.statusErr
}
func (f *fakeRetestBeadsClient) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}
func (f *fakeRetestBeadsClient) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	return nil
}
func (f *fakeRetestBeadsClient) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	return "", nil
}
func (f *fakeRetestBeadsClient) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}
func (f *fakeRetestBeadsClient) RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, labels)
	return nil
}
func (f *fakeRetestBeadsClient) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	return nil
}
func (f *fakeRetestBeadsClient) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	return nil
}
func (f *fakeRetestBeadsClient) AddDependency(projectPath, repoName string, beadID, dependsOn types.BeadID) error {
	return nil
}

// retestGitRepo builds origin + project clone with base branch "development"
// and a hydra/wt-proj-abc worktree branch. conflict=true makes the branch
// edit the same line as a later base commit (unmergeable), false adds a new
// file (provably clean).
func retestGitRepo(t *testing.T, conflict bool) (projectPath string) {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	run := func(dir string, args ...string) {
		t.Helper()
		full := append([]string{"-C", dir}, args...)
		if _, err := gitOut(".", full...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if out, err := gitOut(".", "init", "--bare", "-b", "development", origin); err != nil {
		t.Fatalf("init origin: %v %s", err, out)
	}
	seed := filepath.Join(base, "seed")
	if out, err := gitOut(".", "clone", origin, seed); err != nil {
		t.Fatalf("clone seed: %v %s", err, out)
	}
	run(seed, "config", "user.email", "retest@hydra.local")
	run(seed, "config", "user.name", "retest")
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(seed, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc main() {}\n")
	run(seed, "add", ".")
	run(seed, "commit", "-m", "seed")
	run(seed, "push", "origin", "development")

	// Worktree branch off the seed commit.
	run(seed, "checkout", "-b", "hydra/wt-proj-abc")
	if conflict {
		write("main.go", "package main\n\nfunc main() { println(\"branch\") }\n")
	} else {
		write("feature.go", "package main\n\nfunc Feature() {}\n")
	}
	run(seed, "add", ".")
	run(seed, "commit", "-m", "wt work")
	run(seed, "push", "origin", "hydra/wt-proj-abc")

	// Base advances: same line touched (conflict) or unrelated file (clean).
	run(seed, "checkout", "development")
	if conflict {
		write("main.go", "package main\n\nfunc main() { println(\"base\") }\n")
	} else {
		write("base.go", "package main\n\nfunc Base() {}\n")
	}
	run(seed, "add", ".")
	run(seed, "commit", "-m", "base advances")
	run(seed, "push", "origin", "development")

	projectPath = filepath.Join(base, "project")
	if out, err := gitOut(".", "clone", origin, projectPath); err != nil {
		t.Fatalf("clone project: %v %s", err, out)
	}
	run(projectPath, "config", "user.email", "retest@hydra.local")
	run(projectPath, "config", "user.name", "retest")
	run(projectPath, "checkout", "development")
	// Copy the worktree branch ref into the project clone.
	run(projectPath, "fetch", "origin", "hydra/wt-proj-abc")
	run(projectPath, "update-ref", "refs/heads/hydra/wt-proj-abc", "FETCH_HEAD")
	return projectPath
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := gitOut(".", full...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// TestIntegrationRetestCleanLands drives a full sweep over a real repo whose
// wt branch merges cleanly: gates fail open (no detectable stack), the
// dogfood PR path is invoked from the scratch worktree, and the bead is
// closed with labels stripped.
func TestIntegrationRetestCleanLands(t *testing.T) {
	if testing.Short() {
		t.Skip("temp git repos")
	}
	projectPath := retestGitRepo(t, false)
	beads := &fakeRetestBeadsClient{}
	var landCalls []string
	s := NewIntegrationRetestStage(IntegrationRetestConfig{
		Enabled: true, MaxAttempts: 5, RetryInterval: "1ms",
	}, &Config{Projects: []ProjectConfig{{Name: "proj", Path: projectPath}}},
		beads, nil, nil, t.TempDir())
	s.now = time.Now
	s.landFn = func(projectPath, base, featureBranch, title, token string) (string, error) {
		// The push must come from the scratch worktree, never the project
		// clone's main checkout.
		if _, err := os.Stat(filepath.Join(projectPath, ".git")); err != nil {
			t.Errorf("land called outside a git worktree: %s", projectPath)
		}
		if !strings.HasPrefix(featureBranch, "dogfood/proj-abc-retest") {
			t.Errorf("feature branch = %q, want dogfood/proj-abc-retest", featureBranch)
		}
		landCalls = append(landCalls, featureBranch)
		return "https://github.com/o/r/pull/280", nil
	}
	bead := types.Bead{ID: "proj-abc", Title: "feature", Status: types.StatusOpen,
		Labels: []string{retestLabelFailed}, Project: "proj"}
	beadsList := []types.Bead{bead}
	// ListBeads on the fake returns the parked bead.
	beads.list = beadsList

	s.Run([]types.Project{{Name: "proj", Path: projectPath}}, nil)

	if len(landCalls) != 1 {
		t.Fatalf("land calls = %d, want 1", len(landCalls))
	}
	beads.mu.Lock()
	defer beads.mu.Unlock()
	if len(beads.closed) != 1 || !strings.Contains(beads.closed[0], "pull/280") {
		t.Fatalf("closed = %v, want one close citing PR 280", beads.closed)
	}
	if len(beads.removed) != 1 || len(beads.removed[0]) != 2 {
		t.Fatalf("labels removed = %v, want [integration-failed integration-exhausted]", beads.removed)
	}
}

// TestIntegrationRetestConflictedStaysParked: a branch that still conflicts
// must never reach the land path, stays open, and consumes one attempt.
func TestIntegrationRetestConflictedStaysParked(t *testing.T) {
	if testing.Short() {
		t.Skip("temp git repos")
	}
	projectPath := retestGitRepo(t, true)
	beads := &fakeRetestBeadsClient{}
	s := NewIntegrationRetestStage(IntegrationRetestConfig{
		Enabled: true, MaxAttempts: 5, RetryInterval: "1ms",
	}, &Config{Projects: []ProjectConfig{{Name: "proj", Path: projectPath}}},
		beads, nil, nil, t.TempDir())
	s.now = time.Now
	s.landFn = func(projectPath, base, featureBranch, title, token string) (string, error) {
		t.Errorf("land must not be called on a conflicted branch")
		return "", nil
	}
	beads.list = []types.Bead{{ID: "proj-abc", Title: "feature", Status: types.StatusOpen,
		Labels: []string{retestLabelExhausted}, Project: "proj"}}

	s.Run([]types.Project{{Name: "proj", Path: projectPath}}, nil)

	beads.mu.Lock()
	defer beads.mu.Unlock()
	if len(beads.closed) != 0 {
		t.Fatalf("bead must stay parked, closed = %v", beads.closed)
	}
	if got := s.attempts["proj-abc"]; got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}
