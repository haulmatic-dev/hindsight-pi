package core

import (
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

func TestRepoHasChanges(t *testing.T) {
	// Non-git directory: fail open (true) so a close is never blocked on
	// inability to check.
	if ch, _ := repoHasChanges(t.TempDir(), nil); !ch {
		t.Fatalf("non-git dir should fail open to true")
	}

	repo := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")

	// Clean working tree -> no changes.
	if ch, err := repoHasChanges(repo, nil); err != nil || ch {
		t.Fatalf("clean repo: want (false,nil), got (%v,%v)", ch, err)
	}

	// Untracked file -> changes.
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ch, err := repoHasChanges(repo, nil); err != nil || !ch {
		t.Fatalf("dirty repo: want (true,nil), got (%v,%v)", ch, err)
	}

	// Scoped to an unrelated path -> no changes.
	if ch, _ := repoHasChanges(repo, []string{"other.go"}); ch {
		t.Fatalf("scoped to unrelated file should report no changes")
	}
	// Scoped to the changed path -> changes.
	if ch, _ := repoHasChanges(repo, []string{"x.go"}); !ch {
		t.Fatalf("scoped to changed file should report changes")
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
