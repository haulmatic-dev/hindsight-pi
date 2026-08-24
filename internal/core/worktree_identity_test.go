package core

import (
	"os"
	"path/filepath"
	"testing"
)

// initWtRepo makes a minimal git repo with one commit. (gitRun lives in publisher_test.go.)
func initWtRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "init")
}

// TestIsOurWorktree_SubmoduleGitdir reproduces the 2026-08-24 wedge: the hydra
// repo is a submodule of /opt/dev-tools, so its gitdir lives at
// <super>/.git/modules/components/hydra. A linked worktree's .git pointer then
// contains "/.git/modules/.../worktrees/", and worktreeSourceRepo's naive
// "/.git/worktrees/" marker matched the SUPER repo's .git first, returning the
// super repo path — the guard concluded "valid git worktree of a different
// repository" and refused every spawn (hydra-x9e: 67 retries).
func TestIsOurWorktree_SubmoduleGitdir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub-src")
	initWtRepo(t, sub)

	super := filepath.Join(root, "super")
	initWtRepo(t, super)
	gitRun(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "components/sub")
	gitRun(t, super, "commit", "-qm", "add submodule")

	subCheckout := filepath.Join(super, "components", "sub")
	wt := filepath.Join(root, "sub-wt")
	gitRun(t, subCheckout, "worktree", "add", "-q", wt, "-b", "wt-test")

	if !isOurWorktree(subCheckout, wt) {
		t.Fatalf("linked worktree of a submodule checkout must be recognized as ours")
	}
}

// TestIsOurWorktree_ForeignRepo guards the fail-closed direction: a worktree of
// a genuinely different repository must still be refused.
func TestIsOurWorktree_ForeignRepo(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	initWtRepo(t, a)
	initWtRepo(t, b)
	wt := filepath.Join(root, "b-wt")
	gitRun(t, b, "worktree", "add", "-q", wt, "-b", "wt-test")

	if isOurWorktree(a, wt) {
		t.Fatalf("worktree of repo b must not be recognized as belonging to repo a")
	}
}
