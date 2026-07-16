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
