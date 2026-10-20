package core

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestBeadTargetFiles(t *testing.T) {
	cases := []struct {
		name string
		bead types.Bead
		want int
	}{
		{"metadata files", types.Bead{Metadata: map[string]string{"files": "a.go, b.go"}}, 2},
		{"metadata target_files", types.Bead{Metadata: map[string]string{"target_files": "x/a.go, x/b.go"}}, 2},
		{"metadata files takes precedence", types.Bead{Metadata: map[string]string{"files": "a.go", "target_files": "x/a.go"}}, 1},
		{"description", types.Bead{Description: "Do X\nFiles: internal/x.go internal/y.go\nmore text"}, 2},
		{"tbd-ignored", types.Bead{Description: "Files: TBD"}, 0},
		{"none", types.Bead{Description: "no files declared here"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := beadTargetFiles(&tc.bead); len(got) != tc.want {
				t.Fatalf("want %d files, got %d (%v)", tc.want, len(got), got)
			}
		})
	}
}

func TestVerifyBeforeClose_OffScopeBlock(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	writeAndCommit(t, repo, "x/a.go", "package x\n// modified\n")
	writeAndCommit(t, repo, "y/b.go", "package y\n// modified\n")

	err := closer.verifyBeforeClose(bead, repo, "")
	if err == nil {
		t.Fatal("expected rejection for off-scope changes")
	}
	if !strings.Contains(err.Error(), "y/b.go") {
		t.Fatalf("error should contain off-scope path y/b.go, got: %v", err)
	}
	if !strings.Contains(err.Error(), "off-scope changes") {
		t.Fatalf("error should contain 'off-scope changes', got: %v", err)
	}
}

func TestVerifyBeforeClose_OffScopeWarn(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "warn")
	writeAndCommit(t, repo, "x/a.go", "package x\n// modified\n")
	writeAndCommit(t, repo, "y/b.go", "package y\n// modified\n")

	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("warn mode should not reject close: %v", err)
	}
}

func TestVerifyBeforeClose_OnScopePasses(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	writeAndCommit(t, repo, "x/a.go", "package x\n// modified\n")
	writeAndCommit(t, repo, "x/a_test.go", "package x\n// test\n")

	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("on-scope changes should not reject close: %v", err)
	}
}

func TestVerifyBeforeClose_EmptyTargetsBypasses(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	bead.Metadata = nil
	bead.Description = "no targets declared"
	writeAndCommit(t, repo, "y/b.go", "package y\n// modified\n")

	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("empty targets should bypass scope gate: %v", err)
	}
}

func TestVerifyBeforeClose_ChangedFilesErrorFailsClosed(t *testing.T) {
	// FAIL-CLOSED (2026-10-20 zero-work cascade): a git error reading the
	// evidence repo means the work cannot be verified — the close is
	// rejected (cannot-verify-diff), never accepted. Previously the error
	// was logged "not blocking" and nil changed files passed hasTargetChanges
	// as "unknown, fail open", letting zero-work closes through.
	// Use a non-git directory so changedFiles returns an error.
	nonGitRepo := t.TempDir()
	closer := newScopeGateCloser(t, nonGitRepo, "block")
	bead := &types.Bead{
		ID:       "scope-test-bead",
		Title:    "Test bead",
		Project:  "test-project",
		Metadata: map[string]string{"files": "x/a.go"},
	}

	err := closer.verifyBeforeClose(bead, nonGitRepo, "")
	if err == nil {
		t.Fatal("changedFiles error must fail CLOSED: close must be rejected, not accepted")
	}
	if !strings.Contains(err.Error(), "cannot-verify-diff") {
		t.Fatalf("rejection reason must be cannot-verify-diff, got: %v", err)
	}
}

// TestVerifyBeforeClose_ChecksWorktreeNotCheckout is the regression for the
// empty-diff false-rejection bug: with worktree isolation active the worker
// writes to the chain's worktree, not the shared checkout. The gate must
// inspect the worktree — checking the (clean) checkout rejected correct work
// as "empty-diff".
func TestVerifyBeforeClose_ChecksWorktreeNotCheckout(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "internal/core/seed.go", "package core\n")

	wm := NewWorktreeManager()
	bead := &types.Bead{
		ID:       "wt-bead-1",
		Title:    "GREEN: Implement activity log",
		Project:  "test-project",
		Metadata: map[string]string{"files": "internal/core/activity_log.go"},
	}
	key := string(bead.ID)

	// Create the chain's worktree and write the target file INTO it (as a
	// worker would), leaving the shared checkout clean.
	wt, err := wm.EnsureWorktree(repo, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	full := filepath.Join(wt, "internal/core/activity_log.go")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("package core\n// activity log\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Validation.Enabled = true
	cfg.Validation.RequireDiffOnClose = true
	cfg.Validation.EnforceTargetScope = "block"
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: repo, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
	closer.SetWorktreeManager(wm,
		func(b types.Bead) string { return string(b.ID) },
		func(id types.BeadID) bool { return false },
	)

	// The change lives only in the worktree; the checkout is clean. The gate
	// must find it there and accept.
	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("worktree-isolated bead with the target file written to its worktree must NOT be rejected: %v", err)
	}
}

func setupScopeGateTest(t *testing.T, mode string) (string, *BeadCloser, *types.Bead) {
	t.Helper()
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "x/a.go", "package x\n")
	writeAndCommit(t, repo, "y/b.go", "package y\n")

	bead := &types.Bead{
		ID:       "scope-test-bead",
		Title:    "Test bead",
		Project:  "test-project",
		Metadata: map[string]string{"files": "x/a.go"},
	}
	closer := newScopeGateCloser(t, repo, mode)
	return repo, closer, bead
}

func newScopeGateCloser(t *testing.T, repoPath, mode string) *BeadCloser {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Validation.Enabled = true
	cfg.Validation.EnforceTargetScope = mode
	cfg.Validation.RequireDiffOnClose = true

	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: repoPath, Type: types.RepoTypeSingle},
	})

	c := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
	// Session base = the repo's HEAD at fixture time: without this the
	// session-scoped evidence guard would treat the fixture's own setup
	// commits as worker evidence.
	base, berr := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output()
	if berr != nil {
		// Broken-repo fixtures (ChangedFilesErrorFailsOpen): the "HEAD"
		// sentinel resolves at close time and fails open there too.
		c.SetSessionBase("scope-test-bead", "HEAD")
	} else {
		c.SetSessionBase("scope-test-bead", strings.TrimSpace(string(base)))
	}
	return c
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	return repo
}

func writeAndCommit(t *testing.T, repo, path, content string) {
	t.Helper()
	full := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("add", path)
	git("commit", "-m", "add "+path)
}

func modifyFile(t *testing.T, repo, path, content string) {
	t.Helper()
	full := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureLogOutput redirects the default log output to a buffer for the
// duration of the test and returns the captured bytes.
func captureLogOutput(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(old)
		log.SetFlags(oldFlags)
	}()
	fn()
	return buf.String()
}

// TestVerifyBeforeClose_MainCheckoutLeak reproduces incident 2jjz8: a worker
// wrote into the main checkout instead of its worktree. The worktree is clean,
// but the main checkout holds the changed target file. The diff gate must pass
// because the main-checkout changes are unioned in, and each leaked file must
// be logged.
func TestVerifyBeforeClose_MainCheckoutLeak(t *testing.T) {
	projectPath := initGitRepoWithCommit(t)

	wtMgr := NewWorktreeManager()
	key := "test-leak"
	if _, err := wtMgr.EnsureWorktree(projectPath, key); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	wtPath, ok := wtMgr.ExistingWorktree(projectPath, key)
	if !ok {
		t.Fatal("worktree was not created")
	}

	target := "feature.go"
	writeFile(t, projectPath, target, "leaked content\n")
	if _, err := os.Stat(filepath.Join(wtPath, target)); err == nil {
		t.Fatalf("worktree unexpectedly contains %s", target)
	}

	bead := types.Bead{
		ID:       "test-project-leak",
		Title:    "GREEN: implement feature",
		Project:  "test-project",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Metadata: map[string]string{"files": target},
	}

	cfg := DefaultConfig()
	cfg.Validation.RequireDiffOnClose = true
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: projectPath, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
	closer.SetWorktreeManager(wtMgr,
		func(_ types.Bead) string { return key },
		func(_ types.BeadID) bool { return false },
	)

	logs := captureLogOutput(t, func() {
		if err := closer.verifyBeforeClose(&bead, projectPath, ""); err != nil {
			t.Fatalf("verifyBeforeClose returned error: %v", err)
		}
	})

	want := "[VerifiedClose] test-project-leak main-checkout leak: feature.go"
	if !strings.Contains(logs, want) {
		t.Errorf("expected leak log %q, got:\n%s", want, logs)
	}
}

func TestVerifyBeforeClose_TargetLessImplementationWarns(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	bead.Metadata = nil
	bead.Description = "no targets declared"
	bead.Type = "fr"
	writeAndCommit(t, repo, "internal/core/x.go", "package x\n// modified\n")

	logs := captureLogOutput(t, func() {
		if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
			t.Fatalf("target-less warning must not reject close: %v", err)
		}
	})

	if !strings.Contains(logs, "declared no Target Files") {
		t.Fatalf("expected log containing 'declared no Target Files', got:\n%s", logs)
	}
	if !strings.Contains(logs, "internal/core/x.go") {
		t.Fatalf("expected log containing 'internal/core/x.go', got:\n%s", logs)
	}
}

func TestVerifyBeforeClose_TargetLessEpicNoWarning(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	bead.Metadata = nil
	bead.Description = "no targets declared"
	bead.Type = "epic"
	writeAndCommit(t, repo, "internal/core/x.go", "package x\n// modified\n")

	logs := captureLogOutput(t, func() {
		if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
			t.Fatalf("target-less epic must not reject close: %v", err)
		}
	})

	if strings.Contains(logs, "declared no Target Files") {
		t.Fatalf("epic should not emit target-less warning, got:\n%s", logs)
	}
}

func TestVerifyBeforeClose_TargetLessAllowlistedOnlyNoWarning(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	bead.Metadata = nil
	bead.Description = "no targets declared"
	bead.Type = "implement"
	writeAndCommit(t, repo, "CHANGELOG.md", "# Changelog\n## Added\n")
	writeAndCommit(t, repo, "docs/PRDs/x.md", "# PRD\n")

	logs := captureLogOutput(t, func() {
		if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
			t.Fatalf("target-less allowlisted-only must not reject close: %v", err)
		}
	})

	if strings.Contains(logs, "declared no Target Files") {
		t.Fatalf("allowlisted-only changes should not emit target-less warning, got:\n%s", logs)
	}
}

func TestVerifyBeforeClose_DeclaredTargetsUnaffected(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	// bead already has declared targets from setupScopeGateTest.
	modifyFile(t, repo, "x/a.go", "package x\n// modified\n")
	modifyFile(t, repo, "y/b.go", "package y\n// modified\n")

	logs := captureLogOutput(t, func() {
		if err := closer.verifyBeforeClose(bead, repo, ""); err == nil {
			t.Fatal("expected rejection for off-scope changes with declared targets")
		}
	})

	if strings.Contains(logs, "declared no Target Files") {
		t.Fatalf("declared-target bead should not emit target-less warning, got:\n%s", logs)
	}
}

func TestVerifyBeforeClose_AlreadyIntegratedTargetPasses(t *testing.T) {
	// 5k33p class: the worker's commits were integrated into the working branch
	// before verified-close ran. The chain worktree is clean, but the main
	// checkout contains a commit touching the bead's target since the merge base.
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	wtMgr := NewWorktreeManager()
	key := "already-integrated"
	wtPath, err := wtMgr.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	target := "feature.go"
	writeFile(t, projectPath, target, "integrated content\n")
	runGit(t, projectPath, "add", target)
	runGit(t, projectPath, "commit", "-m", "integrate target")

	// Worktree must be clean (no uncommitted or ahead-of-base changes).
	st, _ := exec.Command("git", "-C", wtPath, "status", "--porcelain").Output()
	if strings.TrimSpace(string(st)) != "" {
		t.Fatalf("precondition: worktree must be clean, got %q", st)
	}

	bead := types.Bead{
		ID:       "test-project-integrated",
		Title:    "GREEN: implement feature",
		Project:  "test-project",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Metadata: map[string]string{"files": target},
	}

	cfg := DefaultConfig()
	cfg.Validation.RequireDiffOnClose = true
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: projectPath, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
	closer.SetWorktreeManager(wtMgr,
		func(_ types.Bead) string { return key },
		func(_ types.BeadID) bool { return false },
	)

	logs := captureLogOutput(t, func() {
		if err := closer.verifyBeforeClose(&bead, projectPath, ""); err != nil {
			t.Fatalf("already-integrated target must not reject close: %v", err)
		}
	})

	want := "[VerifiedClose] test-project-integrated"
	if !strings.Contains(logs, want) {
		t.Errorf("expected integration log %q, got:\n%s", want, logs)
	}
}

func TestVerifyBeforeClose_BothCleanEmptyDiffRejectUnchanged(t *testing.T) {
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	wtMgr := NewWorktreeManager()
	key := "both-clean"
	if _, err := wtMgr.EnsureWorktree(projectPath, key); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	bead := types.Bead{
		ID:       "test-project-clean",
		Title:    "GREEN: implement feature",
		Project:  "test-project",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Metadata: map[string]string{"files": "feature.go"},
	}

	cfg := DefaultConfig()
	cfg.Validation.RequireDiffOnClose = true
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: projectPath, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
	closer.SetWorktreeManager(wtMgr,
		func(_ types.Bead) string { return key },
		func(_ types.BeadID) bool { return false },
	)

	err := closer.verifyBeforeClose(&bead, projectPath, "")
	if err == nil {
		t.Fatal("expected empty-diff rejection when both worktree and main checkout are clean")
	}
	if !strings.Contains(err.Error(), "empty-diff") {
		t.Fatalf("expected empty-diff rejection, got: %v", err)
	}
}

func TestVerifyBeforeClose_NoChainBranchSkipAlreadyIntegratedCheck(t *testing.T) {
	// No chain worktree/branch exists (e.g. it was merged and deleted). The
	// already-integrated check must be skipped and the existing empty-diff
	// reject must still fire.
	projectPath := initGitRepoWithCommit(t)

	bead := types.Bead{
		ID:       "test-project-no-chain",
		Title:    "GREEN: implement feature",
		Project:  "test-project",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Metadata: map[string]string{"files": "feature.go"},
	}

	cfg := DefaultConfig()
	cfg.Validation.RequireDiffOnClose = true
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: projectPath, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)

	err := closer.verifyBeforeClose(&bead, projectPath, "")
	if err == nil {
		t.Fatal("expected empty-diff rejection when no chain branch exists")
	}
	if !strings.Contains(err.Error(), "empty-diff") {
		t.Fatalf("expected empty-diff rejection, got: %v", err)
	}
}

func TestVerifyBeforeClose_IntegratedChainBranchAncestorPasses(t *testing.T) {
	// dy0/ig9hw class: after the chain branch is merged into main, the
	// worktree is clean and the merge base has moved past the work. A direct
	// ancestry check must treat the bead as already integrated.
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	wtMgr := NewWorktreeManager()
	key := "ancestor-integrated"
	wtPath, err := wtMgr.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	target := "feature.go"

	// Worker writes and commits the target in the chain worktree.
	writeFile(t, wtPath, target, "package main\n")
	runGit(t, wtPath, "add", target)
	runGit(t, wtPath, "commit", "-m", "worker commit")

	// Chain branch is merged into main (integrate-into-chain succeeded).
	chainBranch := currentBranch(wtPath)
	runGit(t, projectPath, "merge", chainBranch, "--no-ff", "-m", "integrate chain")

	bead := types.Bead{
		ID:       "test-project-ancestor",
		Title:    "GREEN: implement feature",
		Project:  "test-project",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Metadata: map[string]string{"files": target},
	}

	cfg := DefaultConfig()
	cfg.Validation.RequireDiffOnClose = true
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: projectPath, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
	closer.SetWorktreeManager(wtMgr,
		func(_ types.Bead) string { return key },
		func(_ types.BeadID) bool { return false },
	)

	logs := captureLogOutput(t, func() {
		if err := closer.verifyBeforeClose(&bead, projectPath, ""); err != nil {
			t.Fatalf("ancestor-integrated chain branch must not reject close: %v", err)
		}
	})

	want := "[VerifiedClose] test-project-ancestor chain branch"
	if !strings.Contains(logs, want) {
		t.Errorf("expected ancestry integration log containing %q, got:\n%s", want, logs)
	}
}

func TestVerifyBeforeClose_PartialIntegrationLFAFallback(t *testing.T) {
	// The chain branch is NOT an ancestor of main, but the bead's target
	// file already changed on main since the two branches diverged. The
	// per-target merge-base fallback (not the ancestry shortcut) must pass.
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	wtMgr := NewWorktreeManager()
	key := "partial-lfa"
	wtPath, err := wtMgr.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	// Chain branch carries a unique commit so it is not an ancestor of main.
	writeFile(t, wtPath, "chain_only.go", "package main\n")
	runGit(t, wtPath, "add", "chain_only.go")
	runGit(t, wtPath, "commit", "-m", "chain-only work")

	// Target file lands on main after the chain diverged.
	target := "feature.go"
	writeFile(t, projectPath, target, "package main\n")
	runGit(t, projectPath, "add", target)
	runGit(t, projectPath, "commit", "-m", "target on main")

	bead := types.Bead{
		ID:       "test-project-partial",
		Title:    "GREEN: implement feature",
		Project:  "test-project",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Metadata: map[string]string{"files": target},
	}

	cfg := DefaultConfig()
	cfg.Validation.RequireDiffOnClose = true
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: projectPath, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
	closer.SetWorktreeManager(wtMgr,
		func(_ types.Bead) string { return key },
		func(_ types.BeadID) bool { return false },
	)

	logs := captureLogOutput(t, func() {
		if err := closer.verifyBeforeClose(&bead, projectPath, ""); err != nil {
			t.Fatalf("partial integration fallback must not reject close: %v", err)
		}
	})

	want := "[VerifiedClose] test-project-partial targets already integrated on main"
	if !strings.Contains(logs, want) {
		t.Errorf("expected fallback integration log %q, got:\n%s", want, logs)
	}
}

// withOriginSelfRemote points origin/<branch> at the repo itself so the
// committed-evidence merge-base resolves in tests (campaign branches always
// have an upstream in production).
func withOriginSelfRemote(t *testing.T, repo string) {
	t.Helper()
	branch := strings.TrimSpace(vcGitOut(t, repo, "rev-parse", "--abbrev-ref", "HEAD"))
	gitTestRun(t, repo, "remote", "add", "origin", repo)
	gitTestRun(t, repo, "fetch", "origin", branch)
	gitTestRun(t, repo, "branch", "--set-upstream-to=origin/"+branch, branch)
}

func gitTestRun(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := append([]string{"git", "-C", repo}, args...)
	if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

func vcGitOut(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := append([]string{"git", "-C", repo}, args...)
	out, err := exec.Command(cmd[0], cmd[1:]...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// Regression (2026-09-16, nipuna campaign incident): a worker that ran in
// the MAIN CHECKOUT (no worktree) edited files, exited, and verified-close
// counted the UNCOMMITTED edits as diff evidence. The close was accepted and
// the files were reverted afterwards — a false close with zero landed work.
// In main-checkout mode only COMMITTED changes may satisfy the diff gate.
func TestVerifyBeforeClose_MainCheckoutUncommittedRejected(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "off")
	withOriginSelfRemote(t, repo)
	modifyFile(t, repo, "x/a.go", "package x\n// uncommitted edit\n")

	err := closer.verifyBeforeClose(bead, repo, "")
	if err == nil {
		t.Fatal("uncommitted-only main-checkout change must NOT satisfy the diff gate (revertible evidence)")
	}
	if !strings.Contains(err.Error(), "empty-diff") {
		t.Fatalf("want empty-diff rejection, got: %v", err)
	}
}

// Committed work on the campaign branch still satisfies the gate in
// main-checkout mode (positive control for the guard above).
func TestVerifyBeforeClose_MainCheckoutCommittedAccepted(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "off")
	withOriginSelfRemote(t, repo)
	writeAndCommit(t, repo, "x/a.go", "package x\n// committed campaign work\n")

	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("committed main-checkout change should pass, got: %v", err)
	}
}

// Regression (2026-09-16, nipuna campaign): a worker ran in a chain worktree,
// produced ZERO commits (empty worktree branch == base), and verified-close
// ACCEPTED the close because the ancestry fast-path treats "branch is
// ancestor of main" as "already integrated" — indistinguishable from "never
// did anything". The epic burned 12.8M tokens and closed with no landed work.
// Ancestry alone must never satisfy the diff gate; the target-presence check
// on main since the merge-base is required too.
func TestVerifyBeforeClose_EmptyWorktreeBranchRejected(t *testing.T) {
	projectPath := initGitRepoWithCommit(t)

	wtMgr := NewWorktreeManager()
	key := "test-empty-wt"
	if _, err := wtMgr.EnsureWorktree(projectPath, key); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// No edits anywhere: neither the worktree nor the main checkout changes.

	bead := types.Bead{
		ID:       "test-project-empty",
		Title:    "GREEN: implement feature",
		Project:  "test-project",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Metadata: map[string]string{"files": "feature.go"},
	}

	cfg := DefaultConfig()
	cfg.Validation.RequireDiffOnClose = true
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: projectPath, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
	closer.SetWorktreeManager(wtMgr,
		func(_ types.Bead) string { return key },
		func(_ types.BeadID) bool { return false },
	)

	err := closer.verifyBeforeClose(&bead, projectPath, "")
	if err == nil {
		t.Fatal("empty worktree branch (ancestor of main with no commits) must NOT satisfy the diff gate")
	}
	if !strings.Contains(err.Error(), "empty-diff") {
		t.Fatalf("want empty-diff rejection, got: %v", err)
	}
}

// Regression (2026-09-16, nipuna epic): in main-checkout mode with NO
// declared target files, the committed-evidence gate passed on the
// campaign's PRE-EXISTING commits (docs + store) — nothing the worker did.
// Evidence must be scoped to the bead's own session: commits since the
// branch HEAD recorded at spawn.
func TestVerifyBeforeClose_MainCheckoutPreExistingCommitsRejected(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "off")
	withOriginSelfRemote(t, repo)
	// Campaign-base commit that predates the bead's session.
	writeAndCommit(t, repo, "docs/prd.md", "# prd\n")
	closer.SetSessionBase(bead.ID.String(), sessionBaseCommit(t, repo)) // base AFTER the docs commit
	// Worker did nothing.

	err := closer.verifyBeforeClose(bead, repo, "")
	if err == nil {
		t.Fatal("pre-existing campaign commits must not satisfy the diff gate for a no-work session")
	}
	if !strings.Contains(err.Error(), "empty-diff") {
		t.Fatalf("want empty-diff rejection, got: %v", err)
	}
}

func sessionBaseCommit(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(vcGitOut(t, repo, "rev-parse", "HEAD"))
}

// TestVerifyBeforeClose_AlreadyIntegratedCommitAncestry reproduces the
// hydra-7k1 2026-09-16 stranding: the singleton integrate merged the worktree
// branch into the main checkout, the push was rejected (non-fast-forward),
// the bead reopened, and the worktree branch was deleted — leaving an empty
// worktree diff and no branch name for the ancestry check. Verified-close
// must still accept: worktree HEAD is contained in main HEAD via the merge.
func TestVerifyBeforeClose_AlreadyIntegratedCommitAncestry(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "internal/core/seed.go", "package core\n")

	wm := NewWorktreeManager()
	bead := &types.Bead{
		ID:       "wt-bead-merged",
		Title:    "Fix: versioned worktree-key resolution",
		Project:  "test-project",
		Metadata: map[string]string{"files": "internal/core/worktree_keys.go"},
	}
	key := string(bead.ID)

	wt, err := wm.EnsureWorktree(repo, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	writeAndCommit(t, wt, "internal/core/worktree_keys.go", "package core\n// fix\n")

	// Integrate like the singleton close path does: merge the worktree tip
	// into the main checkout, then delete the branch ref while the worktree
	// directory survives (exactly the post-failed-push state).
	wtHead := strings.TrimSpace(vcGitOut(t, wt, "rev-parse", "HEAD"))
	gitTestRun(t, repo, "merge", "--no-ff", "-m", "hydra: integrate test", wtHead)
	branch := strings.TrimSpace(vcGitOut(t, wt, "branch", "--show-current"))
	if branch == "" {
		t.Fatal("worktree should be on a branch")
	}
	gitTestRun(t, repo, "update-ref", "-d", "refs/heads/"+branch)
	// The branch ref is gone from the main repo. Detach the worktree HEAD at
	// its commit so the fixture mirrors the production state (valid HEAD,
	// clean status, no resolvable branch name — the hydra-7k1 worktree after
	// its branch was cleaned up). A worktree whose HEAD symref dangles would
	// report every file as staged-new, which is not the incident state.
	gitTestRun(t, wt, "checkout", "--detach", wtHead)
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).CombinedOutput(); err == nil {
		t.Fatalf("branch ref should be deleted in main repo, still resolves: %s", strings.TrimSpace(string(out)))
	}

	cfg := DefaultConfig()
	cfg.Validation.Enabled = true
	cfg.Validation.RequireDiffOnClose = true
	cfg.Validation.EnforceTargetScope = "block"
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: repo, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
	closer.SetWorktreeManager(wm,
		func(b types.Bead) string { return string(b.ID) },
		func(id types.BeadID) bool { return false },
	)

	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("locally-integrated bead (branch deleted, push pending) must not be rejected empty-diff: %v", err)
	}

	// Negative control: a second bead whose work is NOT merged must still be
	// rejected — the ancestry fallback must not accept unlanded work.
	bead2 := &types.Bead{
		ID:       "wt-bead-unmerged",
		Title:    "Other fix",
		Project:  "test-project",
		Metadata: map[string]string{"files": "internal/core/other.go"},
	}
	wt2, err := wm.EnsureWorktree(repo, string(bead2.ID))
	if err != nil {
		t.Fatalf("EnsureWorktree 2: %v", err)
	}
	writeAndCommit(t, wt2, "internal/core/other.go", "package core\n// other\n")
	// Make its diff vs the base empty without merging: reset the worktree
	// to main HEAD (a worker that reverted its own work leaves this state —
	// nothing landed, nothing to credit).
	mainHead := strings.TrimSpace(vcGitOut(t, repo, "rev-parse", "HEAD"))
	gitTestRun(t, wt2, "reset", "--hard", mainHead)
	if err := closer.verifyBeforeClose(bead2, repo, ""); err == nil {
		t.Fatal("unmerged bead with empty diff must still be rejected (ancestry fallback over-accepts)")
	} else if !strings.Contains(err.Error(), "empty-diff") {
		t.Fatalf("rejection reason should be empty-diff, got: %v", err)
	}
}
