package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

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
