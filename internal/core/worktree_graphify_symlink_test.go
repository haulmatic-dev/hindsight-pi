package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// RED (TDD) tests for the Worktree graphify-out symlink feature.
//
// Contract pinned from the parent acceptance criteria:
//   - After EnsureWorktree on a repo whose main checkout has
//     graphify-out/graph.json, os.Lstat(<worktree>/graphify-out) reports a
//     symlink and reading <worktree>/graphify-out/graph.json succeeds.
//   - The worktree's .gitignore contains the literal line `graphify-out/`.
//   - git -C <worktree> status --porcelain shows no `graphify-out` entry after
//     symlink creation.
//   - When the main checkout lacks graphify-out, no symlink is created and no
//     error is returned.
//
// The feature is NOT yet implemented (RED phase): these tests must fail for
// the missing-feature reason, not compile or setup errors.

// TestWorktreeGraphifySymlink_CreatedAndReadable: symlink exists at the
// worktree, points through to the main checkout's graph.json, and is readable.
func TestWorktreeGraphifySymlink_CreatedAndReadable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	if err := os.MkdirAll(filepath.Join(proj, "graphify-out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "graphify-out", "graph.json"), []byte(`{"nodes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	wt, err := m.EnsureWorktree(proj, "graphify-chain-1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	link := filepath.Join(wt, "graphify-out")
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("os.Lstat(%s): %v — graphify-out symlink not provisioned in worktree", link, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink (mode %v)", link, fi.Mode())
	}

	data, err := os.ReadFile(filepath.Join(link, "graph.json"))
	if err != nil {
		t.Fatalf("read through symlink: %v", err)
	}
	if string(data) != `{"nodes":[]}` {
		t.Fatalf("unexpected graph.json content read through symlink: %q", string(data))
	}

	// The symlink must resolve to the MAIN checkout's graphify-out dir, so
	// graphify output written by the indexer is visible without re-indexing.
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(wt, target)
	}
	resolved = filepath.Clean(resolved)
	if resolved != filepath.Join(proj, "graphify-out") {
		t.Fatalf("symlink target %q resolves to %q, want %q", target, resolved, filepath.Join(proj, "graphify-out"))
	}
}

// TestWorktreeGraphifySymlink_GitignoreLine: the worktree's .gitignore
// contains the literal line `graphify-out/`.
func TestWorktreeGraphifySymlink_GitignoreLine(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	if err := os.MkdirAll(filepath.Join(proj, "graphify-out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "graphify-out", "graph.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	wt, err := m.EnsureWorktree(proj, "graphify-chain-gi")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(wt, ".gitignore"))
	if err != nil {
		t.Fatalf("read worktree .gitignore: %v", err)
	}
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "graphify-out/" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("worktree .gitignore missing literal line %q; got:\n%s", "graphify-out/", string(data))
	}
}

// TestWorktreeGraphifySymlink_StatusClean: git status --porcelain in the
// worktree shows no graphify-out entry after symlink creation.
func TestWorktreeGraphifySymlink_StatusClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	if err := os.MkdirAll(filepath.Join(proj, "graphify-out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "graphify-out", "graph.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	wt, err := m.EnsureWorktree(proj, "graphify-chain-status")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	out, err := exec.Command("git", "-C", wt, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status --porcelain: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "graphify-out") {
			t.Fatalf("worktree status shows graphify-out entry: %q\nfull status:\n%s", line, string(out))
		}
	}
}

// TestWorktreeGraphifySymlink_AbsentGraphifyOut_NoOpNoError: when the main
// checkout lacks graphify-out, EnsureWorktree creates no symlink and returns
// no error.
func TestWorktreeGraphifySymlink_AbsentGraphifyOut_NoOpNoError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "graphify-chain-absent")
	if err != nil {
		t.Fatalf("EnsureWorktree without graphify-out must not error: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(wt, "graphify-out")); !os.IsNotExist(err) {
		t.Fatalf("expected no graphify-out in worktree when absent in main checkout (lstat err: %v)", err)
	}
}
