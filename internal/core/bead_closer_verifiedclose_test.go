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
	modifyFile(t, repo, "x/a.go", "package x\n// modified\n")
	modifyFile(t, repo, "y/b.go", "package y\n// modified\n")

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
	modifyFile(t, repo, "x/a.go", "package x\n// modified\n")
	modifyFile(t, repo, "y/b.go", "package y\n// modified\n")

	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("warn mode should not reject close: %v", err)
	}
}

func TestVerifyBeforeClose_OnScopePasses(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	modifyFile(t, repo, "x/a.go", "package x\n// modified\n")
	modifyFile(t, repo, "x/a_test.go", "package x\n// test\n")

	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("on-scope changes should not reject close: %v", err)
	}
}

func TestVerifyBeforeClose_EmptyTargetsBypasses(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	bead.Metadata = nil
	bead.Description = "no targets declared"
	modifyFile(t, repo, "y/b.go", "package y\n// modified\n")

	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("empty targets should bypass scope gate: %v", err)
	}
}

func TestVerifyBeforeClose_ChangedFilesErrorFailsOpen(t *testing.T) {
	// Use a non-git directory so changedFiles returns an error.
	nonGitRepo := t.TempDir()
	closer := newScopeGateCloser(t, nonGitRepo, "block")
	bead := &types.Bead{
		ID:       "scope-test-bead",
		Title:    "Test bead",
		Project:  "test-project",
		Metadata: map[string]string{"files": "x/a.go"},
	}

	if err := closer.verifyBeforeClose(bead, nonGitRepo, ""); err != nil {
		t.Fatalf("changedFiles error should fail open: %v", err)
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

	return NewBeadCloser(newMockBeadsClient(), nil, resolver, nil, cfg)
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
	modifyFile(t, repo, "internal/core/x.go", "package x\n// modified\n")

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
	modifyFile(t, repo, "internal/core/x.go", "package x\n// modified\n")

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
	modifyFile(t, repo, "CHANGELOG.md", "# Changelog\n## Added\n")
	modifyFile(t, repo, "docs/PRDs/x.md", "# PRD\n")

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

	want := "[VerifiedClose] test-project-integrated targets already integrated on main"
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
