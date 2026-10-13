package core

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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
		backdateWorktree(t, m, projectPath, key) // past the reconciler age floor
	}

	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true }, nil)
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

func TestReconcileOrphanedWorktrees_SkipsRecentlyActive(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()

	key := "live-key"
	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wt, "bead(live): fresh work"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Terminal bead, no active keys — but the worktree was written seconds
	// ago. The age floor must protect it anyway (2026-09-16 webapp-zpf:
	// the sweep deleted a live worker's sandbox when chain state lied).
	integrated, _ := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true }, nil)
	if integrated != 0 {
		t.Fatalf("integrated = %d, want 0 (recently active worktree protected)", integrated)
	}
	if !branchExists(projectPath, m.branchName(key)) {
		t.Fatal("recently active branch must survive the sweep")
	}
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		t.Fatal("recently active worktree must survive the sweep")
	}

	// Once idle past the floor, the same worktree integrates.
	backdateWorktree(t, m, projectPath, key)
	integrated, _ = ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true }, nil)
	if integrated != 1 {
		t.Fatalf("integrated after idle = %d, want 1", integrated)
	}
}

// backdateWorktree ages every file in the key's worktree (and the dir
// itself) past the reconciler's age floor.
func backdateWorktree(t *testing.T, m *WorktreeManager, projectPath, key string) {
	t.Helper()
	wt := m.worktreePath(projectPath, key)
	old := time.Now().Add(-time.Hour)
	err := filepath.WalkDir(wt, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		return os.Chtimes(path, old, old)
	})
	if err != nil {
		t.Fatalf("backdate %s: %v", wt, err)
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

	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, map[string]bool{key: true}, func(string) bool { return true }, nil)
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
	backdateWorktree(t, m, projectPath, key) // past the reconciler age floor

	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true }, nil)
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
	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true }, nil)
	if integrated != 0 || conflicts != 0 {
		t.Fatalf("non-git project should return (0, 0), got (%d, %d)", integrated, conflicts)
	}
}
