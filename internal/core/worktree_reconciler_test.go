package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReconcileOrphanedWorktrees_IntegratesBothBranches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()

	for _, key := range []string{"orphan-a", "orphan-b"} {
		wt, err := m.EnsureWorktree(projectPath, key)
		if err != nil {
			t.Fatalf("EnsureWorktree %s: %v", key, err)
		}
		if err := os.WriteFile(filepath.Join(wt, key+".txt"), []byte(key+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", key, err)
		}
		if _, err := m.Commit(wt, "bead("+key+"): add file"); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}

	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true })
	if integrated != 2 {
		t.Fatalf("integrated = %d, want 2", integrated)
	}
	if conflicts != 0 {
		t.Fatalf("conflicts = %d, want 0", conflicts)
	}

	for _, key := range []string{"orphan-a", "orphan-b"} {
		if branchExists(projectPath, m.branchName(key)) {
			t.Fatalf("branch %s should be deleted", key)
		}
		if _, err := os.Stat(filepath.Join(projectPath, key+".txt")); err != nil {
			t.Fatalf("file %s.txt should be on working branch: %v", key, err)
		}
	}
}

func TestReconcileOrphanedWorktrees_SkipsActiveKey(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()

	key := "active-key"
	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "active.txt"), []byte("active\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := m.Commit(wt, "bead(active): add file"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, map[string]bool{key: true}, func(string) bool { return true })
	if integrated != 0 {
		t.Fatalf("integrated = %d, want 0", integrated)
	}
	if conflicts != 0 {
		t.Fatalf("conflicts = %d, want 0", conflicts)
	}
	if !branchExists(projectPath, m.branchName(key)) {
		t.Fatal("active branch should still exist")
	}
}

func TestReconcileOrphanedWorktrees_CountsConflict(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()

	key := "conflict-key"
	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	// Divergent change on main after the worktree was created.
	if err := os.WriteFile(filepath.Join(projectPath, "shared.txt"), []byte("main content\n"), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	runGit(t, projectPath, "add", "shared.txt")
	runGit(t, projectPath, "commit", "-m", "main shared")

	if err := os.WriteFile(filepath.Join(wt, "shared.txt"), []byte("worktree content\n"), 0o644); err != nil {
		t.Fatalf("write worktree: %v", err)
	}
	if _, err := m.Commit(wt, "bead(conflict): add shared file"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true })
	if integrated != 0 {
		t.Fatalf("integrated = %d, want 0", integrated)
	}
	if conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1", conflicts)
	}
	// Terminal + conflict: branch is quarantined (hydra/quarantine/<key>),
	// not left to retry forever. Work preserved, retry loop ends.
	if branchExists(projectPath, m.branchName(key)) {
		t.Fatal("conflicting terminal branch should be consumed by quarantine")
	}
	if !branchExists(projectPath, "hydra/quarantine/"+key) {
		t.Fatal("conflicting work should be preserved on the quarantine branch")
	}

	got, err := os.ReadFile(filepath.Join(projectPath, "shared.txt"))
	if err != nil {
		t.Fatalf("read shared.txt: %v", err)
	}
	if string(got) != "main content\n" {
		t.Fatalf("working branch shared.txt = %q, want %q", got, "main content\n")
	}
}

func TestReconcileOrphanedWorktrees_NonGitReturnsZero(t *testing.T) {
	projectPath := t.TempDir()
	m := NewWorktreeManager()
	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true })
	if integrated != 0 || conflicts != 0 {
		t.Fatalf("non-git project should return (0, 0), got (%d, %d)", integrated, conflicts)
	}
}
