package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Reconcile must never touch a worktree whose bead is not terminal. The
// activeKeys guard alone was insufficient: parked workers are not "active"
// (observed live: reconcile tried to integrate parked worker gfac's live
// work) and open beads with no worker yet get integrated prematurely
// (observed live: open bead 598's 1,356 uncommitted-scope lines). Both pull
// incomplete work onto the working branch and churn conflicts forever.
func TestReconcileOrphanedWorktrees_SkipsNonTerminalBead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()

	key := "open-bead"
	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := m.Commit(wt, "bead(open): wip"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return false }, nil)
	if integrated != 0 || conflicts != 0 {
		t.Fatalf("non-terminal bead must be skipped, got integrated=%d conflicts=%d", integrated, conflicts)
	}
	if !branchExists(projectPath, m.branchName(key)) {
		t.Fatal("non-terminal bead's branch must survive reconcile")
	}
	if _, err := os.Stat(filepath.Join(projectPath, "wip.txt")); err == nil {
		t.Fatal("non-terminal bead's work must NOT land on the working branch")
	}
}

// Fail-closed: without a terminal-status predicate reconcile must not touch
// anything — guessing about live work is how premature merges happen.
func TestReconcileOrphanedWorktrees_NilPredicateSkipsAll(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()

	key := "unknown-bead"
	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := m.Commit(wt, "bead(unknown): x"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, nil, nil)
	if integrated != 0 || conflicts != 0 {
		t.Fatalf("nil predicate = fail-closed, got integrated=%d conflicts=%d", integrated, conflicts)
	}
	if !branchExists(projectPath, m.branchName(key)) {
		t.Fatal("nil predicate must leave branches untouched")
	}
}

// A terminal bead whose branch conflicts must not retry-integrate forever
// (observed live: 23 stale branches → "conflicts=23" every sweep, for weeks).
// Quarantine instead: work preserved on hydra/quarantine/<key>, worktree and
// wt branch removed, loop ends.
func TestReconcileOrphanedWorktrees_QuarantinesTerminalConflict(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()

	key := "conflicted-closed"
	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectPath, "shared.txt"), []byte("main content\n"), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	runGit(t, projectPath, "add", "shared.txt")
	runGit(t, projectPath, "commit", "-m", "main shared")

	if err := os.WriteFile(filepath.Join(wt, "shared.txt"), []byte("worktree content\n"), 0o644); err != nil {
		t.Fatalf("write worktree: %v", err)
	}
	if _, err := m.Commit(wt, "bead(conflicted): shared"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	backdateWorktree(t, m, projectPath, key) // past the reconciler age floor

	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true }, nil)
	if integrated != 0 {
		t.Fatalf("conflicted branch cannot integrate, got integrated=%d", integrated)
	}
	if conflicts != 1 {
		t.Fatalf("quarantine path still reports the conflict once, got conflicts=%d", conflicts)
	}
	if branchExists(projectPath, m.branchName(key)) {
		t.Fatal("conflicted wt branch must be consumed by quarantine (no infinite retry)")
	}
	if !branchExists(projectPath, "hydra/quarantine/"+key) {
		t.Fatal("work must be preserved on hydra/quarantine/<key>")
	}
	if isGitWorktree(wt) {
		t.Fatal("worktree must be removed after quarantine")
	}

	// The next sweep must be quiet: no wt branch left to conflict again.
	integrated, conflicts = ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true }, nil)
	if integrated != 0 || conflicts != 0 {
		t.Fatalf("second sweep must be clean, got integrated=%d conflicts=%d", integrated, conflicts)
	}
}
