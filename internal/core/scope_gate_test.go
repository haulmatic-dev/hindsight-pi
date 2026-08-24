package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestBeadTypeShouldDeclareTargets pins the implementation-bead classifier
// used by the target-less scope warning.
func TestBeadTypeShouldDeclareTargets(t *testing.T) {
	cases := []struct {
		beadType string
		want     bool
	}{
		{"fr", true},
		{"fix", true},
		{"implement", true},
		{"green", true},
		{"refactor", true},
		{"task", true},
		{"us-heavy", true},
		{"REFACTOR", true},
		{"  Implement ", true},
		{"epic", false},
		{"approval", false},
		{"discovery", false},
		{"us-light", false},
		{"us-integration", false},
		{"nfr-config", false},
		{"nfr-verification", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := beadTypeShouldDeclareTargets(tc.beadType); got != tc.want {
			t.Errorf("beadTypeShouldDeclareTargets(%q) = %v, want %v", tc.beadType, got, tc.want)
		}
	}
}

func TestScopeViolations(t *testing.T) {
	cases := []struct {
		name    string
		changed []string
		targets []string
		want    []string
	}{
		{
			name:    "changed file matches target",
			changed: []string{"internal/core/a.go"},
			targets: []string{"internal/core/a.go"},
			want:    nil,
		},
		{
			name:    "same-directory test file is allowed",
			changed: []string{"internal/core/a_test.go"},
			targets: []string{"internal/core/a.go"},
			want:    nil,
		},
		{
			name:    "go.mod and go.sum are allowed",
			changed: []string{"go.mod", "go.sum"},
			targets: []string{"internal/core/a.go"},
			want:    nil,
		},
		{
			name:    "empty targets means no scope limit",
			changed: []string{"internal/other/b.go"},
			targets: []string{},
			want:    nil,
		},
		{
			name:    "symbol suffix is stripped before directory allowlist",
			changed: []string{"internal/core/a_test.go"},
			targets: []string{"internal/core/a.go:DoThing()"},
			want:    nil,
		},
		{
			name:    "redundant segments are cleaned before comparison",
			changed: []string{"internal/core/../core/a.go"},
			targets: []string{"internal/core/a.go"},
			want:    nil,
		},
		{
			name:    "off-scope file is a violation",
			changed: []string{"internal/other/b.go"},
			targets: []string{"internal/core/a.go"},
			want:    []string{"internal/other/b.go"},
		},
		{
			name:    "symbol suffix is stripped from target",
			changed: []string{"internal/core/a.go"},
			targets: []string{"internal/core/a.go:DoThing()"},
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scopeViolations(tc.changed, tc.targets)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("scopeViolations(%v, %v) = %v, want %v", tc.changed, tc.targets, got, tc.want)
			}
		})
	}
}

func TestChangedFiles(t *testing.T) {
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

	tracked := filepath.Join(repo, "tracked.go")
	if err := os.WriteFile(tracked, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "tracked.go")
	git("commit", "-m", "initial")

	// Clean working tree should report no changes.
	got, err := changedFiles(repo)
	if err != nil {
		t.Fatalf("changedFiles on clean repo: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("clean repo: want no changes, got %v", got)
	}

	// Modify a tracked file and add an untracked file.
	if err := os.WriteFile(tracked, []byte("package x\n// modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	untracked := filepath.Join(repo, "untracked.go")
	if err := os.WriteFile(untracked, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err = changedFiles(repo)
	if err != nil {
		t.Fatalf("changedFiles: %v", err)
	}
	want := []string{"tracked.go", "untracked.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("changedFiles = %v, want %v", got, want)
	}
}

func TestChangedFiles_Rename(t *testing.T) {
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

	oldFile := filepath.Join(repo, "old.go")
	if err := os.WriteFile(oldFile, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "old.go")
	git("commit", "-m", "initial")

	git("mv", "old.go", "new.go")

	got, err := changedFiles(repo)
	if err != nil {
		t.Fatalf("changedFiles: %v", err)
	}
	want := []string{"new.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("changedFiles = %v, want %v", got, want)
	}
}

func TestChangedFiles_EmptyRepoPath(t *testing.T) {
	if got, err := changedFiles(""); err == nil {
		t.Fatalf("expected error for empty repo path, got %v", got)
	}
}

func TestParseGitStatusPorcelain(t *testing.T) {
	input := " M tracked.go\n?? untracked.go\nR  old.go -> new.go\n"
	want := []string{"tracked.go", "untracked.go", "new.go"}
	got := parseGitStatusPorcelain(input)
	if !slices.Equal(got, want) {
		t.Fatalf("parseGitStatusPorcelain = %v, want %v", got, want)
	}
}

// TestScopeViolations_DocsAllowlisted pins the documentation-discipline
// allowlist: CHANGELOG.md and docs/** updates are mandated by AGENTS.md for
// every change, so the scope gate must never flag them.
func TestScopeViolations_DocsAllowlisted(t *testing.T) {
	targets := []string{"internal/core/a.go"}
	for _, p := range []string{"CHANGELOG.md", "docs/PRDs/x.md", "docs/guide.md"} {
		if v := scopeViolations([]string{p}, targets); len(v) != 0 {
			t.Errorf("%s must be allowlisted, got violation %v", p, v)
		}
	}
	if v := scopeViolations([]string{"docsy/evil.go"}, targets); len(v) != 1 {
		t.Errorf("docsy/ must NOT match the docs/ allowlist prefix")
	}
}

func TestHasTargetChanges(t *testing.T) {
	// Diff unknown: fail open.
	if !hasTargetChanges(nil, nil) {
		t.Fatalf("nil changed slice should fail open to true")
	}

	// No targets, no changes -> no diff.
	if hasTargetChanges([]string{}, nil) {
		t.Fatalf("empty changes with no targets should report no diff")
	}

	// No targets, any change -> diff.
	if !hasTargetChanges([]string{"x.go"}, nil) {
		t.Fatalf("any change with no targets should report diff")
	}

	// Targets declared, no matching change -> no diff.
	if hasTargetChanges([]string{"x.go"}, []string{"other.go"}) {
		t.Fatalf("change to unrelated file should not satisfy target diff")
	}

	// Targets declared, matching change -> diff.
	if !hasTargetChanges([]string{"x.go"}, []string{"x.go"}) {
		t.Fatalf("change to target file should satisfy target diff")
	}

	// Symbol suffix is stripped before matching.
	if !hasTargetChanges([]string{"x.go"}, []string{"x.go:DoThing()"}) {
		t.Fatalf("symbol suffix should be stripped before matching")
	}

	// Cleaned paths match even when input has redundant segments.
	if !hasTargetChanges([]string{"internal/core/../core/x.go"}, []string{"internal/core/x.go"}) {
		t.Fatalf("redundant segments should be cleaned before matching")
	}
}

func BenchmarkScopeViolations(b *testing.B) {
	changed := []string{
		"internal/core/a.go",
		"internal/core/b.go",
		"internal/core/a_test.go",
		"internal/other/c.go",
		"CHANGELOG.md",
		"docs/PRDs/x.md",
		"go.mod",
		"go.sum",
	}
	targets := []string{"internal/core/a.go"}
	for i := 0; i < b.N; i++ {
		_ = scopeViolations(changed, targets)
	}
}

func BenchmarkTargetlessWarningPaths(b *testing.B) {
	changed := []string{
		"internal/core/a.go",
		"internal/core/b.go",
		"CHANGELOG.md",
		"docs/PRDs/x.md",
		"go.mod",
		"go.sum",
	}
	for i := 0; i < b.N; i++ {
		_ = targetlessWarningPaths(changed)
	}
}

// 65wa0 burned 24.9M tokens in an empty-diff reject loop: the worker
// COMMITTED its fix in the chain worktree, leaving a clean working tree —
// and changedFiles only looked at git status (uncommitted). Next worker saw
// the work done, no-oped, rejected again. Committed-ahead-of-base changes
// must count as work.
func TestChangedFiles_IncludesCommittedAheadOfBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()
	wt, err := m.EnsureWorktree(proj, "proj-c1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	base := "master"
	if out, _ := exec.Command("git", "-C", proj, "rev-parse", "--abbrev-ref", "HEAD").Output(); strings.TrimSpace(string(out)) != "" {
		base = strings.TrimSpace(string(out))
	}
	// worker commits its change; working tree clean
	if err := os.WriteFile(filepath.Join(wt, "fixed.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wt, "bead(b1): fix"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	st, _ := exec.Command("git", "-C", wt, "status", "--porcelain").Output()
	if strings.TrimSpace(string(st)) != "" {
		t.Fatalf("precondition: working tree must be clean, got %q", st)
	}

	changed, err := changedFilesWithBase(wt, base)
	if err != nil {
		t.Fatalf("changedFilesWithBase: %v", err)
	}
	found := false
	for _, f := range changed {
		if f == "fixed.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("committed-ahead-of-base change must count, got %v", changed)
	}
}

func TestHasTargetChanges_DirectoryTarget(t *testing.T) {
	// hydra-2l9: bead target "cmd/worker/" (a directory) never exact-matched,
	// so a fully-committed worktree was empty-diff rejected 3x.
	if !hasTargetChanges([]string{"cmd/worker/session_stats.go"}, []string{"cmd/worker/"}) {
		t.Fatalf("file under a directory target should satisfy target diff")
	}
	// Sibling directory must not match.
	if hasTargetChanges([]string{"cmd/hydra/main.go"}, []string{"cmd/worker/"}) {
		t.Fatalf("file in sibling directory should not satisfy directory target")
	}
	// Prefix-without-boundary must not match (cmd/worker2 vs cmd/worker).
	if hasTargetChanges([]string{"cmd/worker2/x.go"}, []string{"cmd/worker/"}) {
		t.Fatalf("partial-name directory must not match without path boundary")
	}
}
