package core

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestWorktreeRoot_NotUnderProjectParent verifies worktrees are rooted under a
// daemon-owned base (HYDRA_HOME), NOT the project's parent dir — which may be
// owned by another user (e.g. a per-user project under /home/<user>) and thus
// not writable by the hydra daemon. That parent-dir mkdir was the bug.
func TestWorktreeRoot_NotUnderProjectParent(t *testing.T) {
	t.Setenv("HYDRA_HOME", "/opt/hydra")
	proj := "/home/alice/projects/source-to-pay/svc"

	root := worktreeRoot(proj)

	if !strings.HasPrefix(root, "/opt/hydra"+string(filepath.Separator)) {
		t.Errorf("worktreeRoot should live under HYDRA_HOME, got %q", root)
	}
	if strings.HasPrefix(root, filepath.Dir(proj)+string(filepath.Separator)) {
		t.Errorf("worktreeRoot must NOT be under the project's parent dir, got %q", root)
	}
}

// TestWorktreeRoot_FallsBackToTemp verifies that without HYDRA_HOME the root is
// still outside the project tree (system temp), never the project parent.
func TestWorktreeRoot_FallsBackToTemp(t *testing.T) {
	t.Setenv("HYDRA_HOME", "")
	proj := "/home/alice/projects/svc"

	root := worktreeRoot(proj)

	if root == "" {
		t.Fatal("worktreeRoot returned empty")
	}
	if strings.HasPrefix(root, "/home/alice/projects") {
		t.Errorf("worktreeRoot must be outside the project tree, got %q", root)
	}
}

// TestWorktreeRoot_UniquePerProject verifies two distinct projects get distinct
// worktree roots (no collision from same basename).
func TestWorktreeRoot_UniquePerProject(t *testing.T) {
	t.Setenv("HYDRA_HOME", "/opt/hydra")
	a := worktreeRoot("/home/alice/projects/svc")
	b := worktreeRoot("/home/bob/projects/svc")
	if a == b {
		t.Errorf("distinct projects must get distinct roots; both = %q", a)
	}
}
