package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// spawnTestSpawner builds a minimal WorkerSpawner for guard tests. It leaves
// the worker binary unset so a guard miss would try to start a real process.
func spawnTestSpawner(ba *BeadAnalyzer) *WorkerSpawner {
	s := NewWorkerSpawner(
		make(map[string]*types.Worker),
		&sync.RWMutex{},
		nil, // workerTracker
		nil, // spawnRateLimiter
		nil, // errorLogger
		NewRetryManager(time.Second, time.Minute),
		&Config{},
		nil, // beadsClient
		ba,
		NewWorkerEnvBuilder(&Config{}),
		nil, // workerLifecycle
		nil, // userContexts
	)
	s.workerLifecycle = NewWorkerLifecycle(
		s.retryManager,
		nil, nil, nil, nil, &sync.RWMutex{},
		make(map[string]bool),
		func(string) {},
		func(*types.Worker, bool, int) error { return nil },
		func() {},
		nil,
		func() int { return 3 },
		nil,
	)
	return s
}

func TestSpawnInternal_RefusesEmptyProjectPath(t *testing.T) {
	ba := NewBeadAnalyzer(nil)
	s := spawnTestSpawner(ba)

	bead := types.Bead{ID: "orphan-xyz", Project: ""}
	_, err := s.SpawnInternal(bead)
	if err == nil {
		t.Fatal("expected spawn refusal for empty project path")
	}
	if !strings.Contains(err.Error(), "empty or relative") {
		t.Fatalf("error should say empty/relative: %v", err)
	}
}

func TestSpawnInternal_RefusesRelativeProjectPath(t *testing.T) {
	ba := NewBeadAnalyzer([]types.Project{
		{Name: "platform", Path: "projects-hydra-repro", Type: types.RepoTypeMulti, Repos: []types.Repo{{Name: "tiny-greeter"}}},
	})
	s := spawnTestSpawner(ba)

	bead := types.Bead{ID: "platform/tiny-greeter-u1i", Project: "platform", Repo: "tiny-greeter"}
	_, err := s.SpawnInternal(bead)
	if err == nil {
		t.Fatal("expected spawn refusal for relative project path")
	}
	if !strings.Contains(err.Error(), "empty or relative") {
		t.Fatalf("error should say empty/relative: %v", err)
	}
}

// removeWorktreeRegistration returns a best-effort cleanup func that force-
// removes the worktree, prunes stale registrations, and deletes the actual
// hydra/wt-* branch checked out there. Live acceptance tests that call
// EnsureWorktree against a REAL fixture repo MUST register it with t.Cleanup:
// a leftover registration makes the next real spawn fail with "branch already
// used by worktree" (git exit 128) until someone prunes it by hand
// (hydra-g4e live-acceptance incident, 2026-09-21).
func removeWorktreeRegistration(repoDir, worktreePath string) func() {
	return func() {
		branch := ""
		if out, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
			branch = strings.TrimSpace(string(out))
		}
		run := func(args ...string) {
			cmd := exec.Command("git", args...)
			cmd.Dir = repoDir
			_ = cmd.Run() // best-effort cleanup: never fail the test on cleanup
		}
		run("worktree", "remove", "--force", worktreePath)
		run("worktree", "prune")
		if strings.HasPrefix(branch, "hydra/wt-") {
			run("branch", "-D", branch)
		}
	}
}

func TestRemoveWorktreeRegistration_CleansFixtureRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	t.Setenv("HYDRA_HOME", t.TempDir())
	m := NewWorktreeManager()
	key := "cleanup-check"

	wt, err := m.EnsureWorktree(proj, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if !strings.Contains(gitDir(t, proj, "worktree", "list"), wt) {
		t.Fatalf("worktree %s not registered in fixture repo", wt)
	}

	removeWorktreeRegistration(proj, wt)()

	if strings.Contains(gitDir(t, proj, "worktree", "list"), wt) {
		t.Fatal("worktree registration left behind in fixture repo")
	}
	if branch := m.branchName(key); strings.TrimSpace(gitDir(t, proj, "branch", "--list", branch)) != "" {
		t.Fatalf("branch %s left behind in fixture repo", branch)
	}
}

// TestSpawnInternal_LiveAcceptanceWorktreeCleanup is the safe, codified version
// of hydra-g4e's ad-hoc live acceptance. It only runs when explicitly enabled
// (HYDRA_LIVE_SPAWN_VERIFY=1, optionally HYDRA_LIVE_SPAWN_REPO=<path>) and MUST
// keep registering removeWorktreeRegistration via t.Cleanup so the real buddhi
// fixture repo never keeps a worktree registration after the run.
func TestSpawnInternal_LiveAcceptanceWorktreeCleanup(t *testing.T) {
	if os.Getenv("HYDRA_LIVE_SPAWN_VERIFY") != "1" {
		t.Skip("set HYDRA_LIVE_SPAWN_VERIFY=1 to run the live worktree-cleanup acceptance against the real fixture repo")
	}
	repoDir := os.Getenv("HYDRA_LIVE_SPAWN_REPO")
	if repoDir == "" {
		repoDir = "/home/buddhi/projects-hydra-repro/tiny-greeter"
	}
	if _, err := os.Stat(repoDir); err != nil {
		t.Skipf("live fixture repo %s unavailable: %v", repoDir, err)
	}

	t.Setenv("HYDRA_HOME", t.TempDir())
	m := NewWorktreeManager()
	wt, err := m.EnsureWorktree(repoDir, "live-spawn-path-cleanup")
	if err != nil {
		t.Fatalf("EnsureWorktree on live fixture repo: %v", err)
	}
	if !filepath.IsAbs(wt) {
		t.Fatalf("live worktree path must be absolute, got %q", wt)
	}
	if !strings.Contains(gitDir(t, repoDir, "worktree", "list"), wt) {
		t.Fatalf("worktree %s not registered in live fixture repo", wt)
	}
	t.Cleanup(removeWorktreeRegistration(repoDir, wt))
	t.Logf("live acceptance worktree created and cleanup registered: %s", wt)
}
