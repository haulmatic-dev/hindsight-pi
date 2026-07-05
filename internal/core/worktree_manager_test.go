package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@hydra.local")
	run("config", "user.name", "hydra-test")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "seed")
	return dir
}

func TestWorktreeManager_Lifecycle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "srv-chain-1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if !isGitWorktree(wt) {
		t.Fatalf("worktree not created at %s", wt)
	}

	// idempotent
	if wt2, err := m.EnsureWorktree(proj, "srv-chain-1"); err != nil || wt2 != wt {
		t.Fatalf("not idempotent: %v %s vs %s", err, wt2, wt)
	}

	// edit + commit in the worktree
	if err := os.WriteFile(filepath.Join(wt, "feature.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if committed, err := m.Commit(wt, "bead(b1): add feature"); err != nil || !committed {
		t.Fatalf("Commit: committed=%v err=%v", committed, err)
	}
	// nothing to commit now
	if committed, _ := m.Commit(wt, "noop"); committed {
		t.Fatal("expected nothing to commit on clean worktree")
	}
	// isolation: main checkout must NOT have the file before integration
	if _, err := os.Stat(filepath.Join(proj, "feature.go")); err == nil {
		t.Fatal("main checkout should not see the file before integrate")
	}

	// integrate + remove
	if err := m.IntegrateAndRemove(proj, "srv-chain-1"); err != nil {
		t.Fatalf("IntegrateAndRemove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "feature.go")); err != nil {
		t.Fatalf("integrated file missing from main checkout: %v", err)
	}
	if isGitWorktree(wt) {
		t.Fatal("worktree should be removed")
	}
	if branchExists(proj, m.branchName("srv-chain-1")) {
		t.Fatal("chain branch should be deleted")
	}
}

func TestWorktreeManager_ConcurrentChainsIsolated(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	wtA, err := m.EnsureWorktree(proj, "chainA")
	if err != nil {
		t.Fatalf("chainA: %v", err)
	}
	wtB, err := m.EnsureWorktree(proj, "chainB")
	if err != nil {
		t.Fatalf("chainB: %v", err)
	}
	if wtA == wtB {
		t.Fatal("chains must get distinct worktrees")
	}

	_ = os.WriteFile(filepath.Join(wtA, "a.go"), []byte("A\n"), 0o644)
	_ = os.WriteFile(filepath.Join(wtB, "b.go"), []byte("B\n"), 0o644)
	if _, err := m.Commit(wtA, "bead(a): a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wtB, "bead(b): b"); err != nil {
		t.Fatal(err)
	}
	// A's file must not be visible in B's worktree
	if _, err := os.Stat(filepath.Join(wtB, "a.go")); err == nil {
		t.Fatal("chainB worktree must not see chainA's file")
	}

	if err := m.IntegrateAndRemove(proj, "chainA"); err != nil {
		t.Fatalf("integrate A: %v", err)
	}
	if err := m.IntegrateAndRemove(proj, "chainB"); err != nil {
		t.Fatalf("integrate B: %v", err)
	}
	for _, f := range []string{"a.go", "b.go"} {
		if _, err := os.Stat(filepath.Join(proj, f)); err != nil {
			t.Fatalf("both chains should integrate: %s missing", f)
		}
	}
}
