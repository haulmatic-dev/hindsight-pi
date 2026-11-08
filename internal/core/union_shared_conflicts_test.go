package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractConflictPaths(t *testing.T) {
	detail := "Auto-merging CHANGELOG.md\nCONFLICT (content): Merge conflict in CHANGELOG.md\nAuto-merging .gitignore\nCONFLICT (content): Merge conflict in .gitignore\nerror: could not apply commit"
	got := extractConflictPaths(detail)
	if len(got) != 2 || got[0] != "CHANGELOG.md" || got[1] != ".gitignore" {
		t.Fatalf("got %v, want [CHANGELOG.md .gitignore]", got)
	}
	if got := extractConflictPaths("some unrelated error"); got != nil {
		t.Fatalf("expected nil for non-conflict detail, got %v", got)
	}
}

// TestUnionResolveSharedConflicts verifies the integrate-side safety net: a
// scratch worktree whose merge conflicts only on CHANGELOG.md is resolved by
// union (both sides' lines survive) and committed.
func TestUnionResolveSharedConflicts(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("CHANGELOG.md", "## [Unreleased]\n- base\n")
	write("code.txt", "one\n")
	run("add", "-A")
	run("commit", "-qm", "base")

	run("checkout", "-q", "-b", "a")
	write("CHANGELOG.md", "## [Unreleased]\n- base\n- side a\n")
	run("commit", "-aqm", "a")

	run("checkout", "-q", "main")
	write("CHANGELOG.md", "## [Unreleased]\n- base\n- side main\n")
	run("commit", "-am", "main")

	if err := exec.Command("git", "-C", dir, "merge", "--no-ff", "--no-commit", "a").Run(); err == nil {
		t.Fatal("expected merge to conflict on CHANGELOG.md")
	}
	// A real conflict only auto-resolves via the union path.
	if !unionResolveSharedConflicts(dir) {
		t.Fatal("unionResolveSharedConflicts returned false for a CHANGELOG.md-only conflict")
	}
	data, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "side a") || !strings.Contains(string(data), "side main") {
		t.Fatalf("union lost a side:\n%s", data)
	}
	if out, err := exec.Command("git", "-C", dir, "diff", "--name-only", "--diff-filter=U").CombinedOutput(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatalf("still unmerged: %s", out)
	}
}

// TestUnionResolveSharedConflictsMixed verifies fail-closed: one unmerged
// non-shared path means no auto-resolve.
func TestUnionResolveSharedConflictsMixed(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v", args, out)
		}
		return string(out)
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("CHANGELOG.md", "- base\n")
	write("main.go", "package main\n")
	run("add", "-A")
	run("commit", "-qm", "base")
	run("checkout", "-q", "-b", "a")
	write("CHANGELOG.md", "- base\n- a\n")
	write("main.go", "package main // a\n")
	run("commit", "-aqm", "a")
	run("checkout", "-q", "main")
	write("CHANGELOG.md", "- base\n- m\n")
	write("main.go", "package main // main\n")
	run("commit", "-am", "main")
	if err := exec.Command("git", "-C", dir, "merge", "--no-ff", "--no-commit", "a").Run(); err == nil {
		t.Fatal("expected conflict")
	}
	if unionResolveSharedConflicts(dir) {
		t.Fatal("must not auto-resolve when a non-shared file is conflicted")
	}
}
