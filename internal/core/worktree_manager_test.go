package core

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestWorktreeManager_EnsureWorktree_RecoversBrokenSymlink(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()
	wtPath := m.worktreePath(proj, "chain-symlink")
	root := worktreeRoot(proj)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/stale-path", wtPath); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	wt, err := m.EnsureWorktree(proj, "chain-symlink")
	log.SetOutput(old)
	if err != nil {
		t.Fatalf("EnsureWorktree should recover broken symlink: %v", err)
	}
	if wt != wtPath {
		t.Fatalf("expected %s, got %s", wtPath, wt)
	}
	if !isGitWorktree(wt) {
		t.Fatalf("recovered path is not a git worktree: %s", wt)
	}
	if !strings.Contains(buf.String(), "[Worktree] recovered stale path") || !strings.Contains(buf.String(), "(prune+recreate)") {
		t.Fatalf("expected recovery log, got: %s", buf.String())
	}
}

func TestWorktreeManager_EnsureWorktree_ReusesValidWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "chain-reuse"

	wt, err := m.EnsureWorktree(proj, key)
	if err != nil {
		t.Fatalf("first EnsureWorktree: %v", err)
	}
	if _, err := gitOut(proj, "rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}

	// Advance main after the worktree was created.
	if err := os.WriteFile(filepath.Join(proj, "main-advance.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, proj, "add", "-A")
	runGitCmd(t, proj, "commit", "-m", "advance main")
	mainHead, err := gitOut(proj, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	wt2, err := m.EnsureWorktree(proj, key)
	log.SetOutput(old)
	if err != nil {
		t.Fatalf("reuse EnsureWorktree: %v", err)
	}
	if wt2 != wt {
		t.Fatalf("reuse should return same path: %s vs %s", wt2, wt)
	}
	wtHead, err := gitOut(wt, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(wtHead) != strings.TrimSpace(mainHead) {
		t.Fatalf("worktree not reset to base: worktree=%s main=%s", wtHead, mainHead)
	}
	if !strings.Contains(buf.String(), "[Worktree] recovered stale path") || !strings.Contains(buf.String(), "(reuse)") {
		t.Fatalf("expected reuse log, got: %s", buf.String())
	}
}

func TestWorktreeManager_EnsureWorktree_ReattachesStaleBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "chain-reattach"

	wt, err := m.EnsureWorktree(proj, key)
	if err != nil {
		t.Fatalf("first EnsureWorktree: %v", err)
	}
	// Leave a commit on the chain branch so the branch tip differs from main.
	if err := os.WriteFile(filepath.Join(wt, "chain.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wt, "chain commit"); err != nil {
		t.Fatalf("commit in worktree: %v", err)
	}

	// Remove the worktree directory but keep the branch (stale branch, absent path).
	runGitCmd(t, proj, "worktree", "remove", wt)
	if _, err := os.Stat(wt); err == nil {
		t.Fatal("worktree path still exists after remove")
	}
	if !branchExists(proj, m.branchName(key)) {
		t.Fatal("chain branch should still exist")
	}

	mainHead, err := gitOut(proj, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	wt2, err := m.EnsureWorktree(proj, key)
	if err != nil {
		t.Fatalf("reattach EnsureWorktree: %v", err)
	}
	if wt2 != wt {
		t.Fatalf("reattach should return same path: %s vs %s", wt2, wt)
	}
	if !isGitWorktree(wt2) {
		t.Fatalf("reattached path is not a worktree: %s", wt2)
	}
	wtHead, err := gitOut(wt2, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(wtHead) != strings.TrimSpace(mainHead) {
		t.Fatalf("reattached worktree not reset to base: worktree=%s main=%s", wtHead, mainHead)
	}
}

func TestWorktreeManager_EnsureWorktree_ForeignWorktreeFailsClosed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projA := gitInitRepo(t)
	projB := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "chain-foreign"
	wtPath := m.worktreePath(projA, key)
	root := worktreeRoot(projA)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a valid worktree of repo B at the path repo A would use.
	runGitCmd(t, projB, "worktree", "add", wtPath)
	if !isGitWorktree(wtPath) {
		t.Fatal("foreign worktree not created")
	}

	_, err := m.EnsureWorktree(projA, key)
	if err == nil {
		t.Fatal("expected error for foreign worktree, got nil")
	}
	if !strings.Contains(err.Error(), "different repository") {
		t.Fatalf("expected different-repo error, got: %v", err)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("foreign worktree path should not have been removed: %v", err)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
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

// TestWorktreeManager_EnsureWorktree_KeepsUnintegratedCommits: the reuse-reset
// must only fire when the worktree tip is already contained in the working
// branch. The unguarded reset rewound branches holding committed worker work
// (hydra-xnx f6269c0/d61ae5d, 2026-09-06) and the integrate then silently
// no-opped while the bead closed.
func TestWorktreeManager_EnsureWorktree_KeepsUnintegratedCommits(t *testing.T) {
	proj := gitInitRepo(t)
	t.Setenv("HYDRA_HOME", t.TempDir())
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "k1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// Worker commits its own work and leaves the worktree clean.
	if err := os.WriteFile(filepath.Join(wt, "worker.txt"), []byte("worker work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, wt, "add", "-A")
	gitDir(t, wt, "commit", "-m", "worker commit")
	workSHA := head(t, wt)

	// Main advances while the worktree sits unintegrated.
	if err := os.WriteFile(filepath.Join(proj, "main2.txt"), []byte("main moves\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, proj, "add", "-A")
	gitDir(t, proj, "commit", "-m", "main advances")

	// Re-ensure: the committed work is NOT contained in HEAD, so no reset.
	wt2, err := m.EnsureWorktree(proj, "k1")
	if err != nil {
		t.Fatalf("EnsureWorktree (reuse): %v", err)
	}
	if got := head(t, wt2); got != workSHA {
		t.Fatalf("reuse-reset destroyed unintegrated commits: head = %s, want %s", got, workSHA)
	}

	// Once the work IS integrated (contained in HEAD), reuse-reset is safe
	// and syncs the worktree back to the base.
	gitDir(t, proj, "merge", "--no-ff", "-m", "integrate k1", m.branchName("k1"))
	wt3, err := m.EnsureWorktree(proj, "k1")
	if err != nil {
		t.Fatalf("EnsureWorktree (post-integrate): %v", err)
	}
	if got, want := head(t, wt3), head(t, proj); got != want {
		t.Fatalf("post-integrate reuse did not sync to base: head = %s, want %s", got, want)
	}
}

// TestWorktreeManager_Integrate_VerifiesAndPreserves: Integrate merges and
// verifies containment but leaves the worktree + branch in place; Remove is
// the explicit teardown after post-merge steps (push) succeed.
func TestWorktreeManager_Integrate_VerifiesAndPreserves(t *testing.T) {
	proj := gitInitRepo(t)
	t.Setenv("HYDRA_HOME", t.TempDir())
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "k2")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "feat.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	committed, err := m.Commit(wt, "feat: k2")
	if err != nil || !committed {
		t.Fatalf("Commit: committed=%v err=%v", committed, err)
	}

	if err := m.Integrate(proj, "k2"); err != nil {
		t.Fatalf("Integrate: %v", err)
	}

	// Merge landed on the working branch...
	if _, err := os.Stat(filepath.Join(proj, "feat.txt")); err != nil {
		t.Fatalf("expected feat.txt on working branch after Integrate: %v", err)
	}
	// ...but the worktree + branch are preserved until Remove.
	if !isGitWorktree(m.worktreePath(proj, "k2")) {
		t.Fatal("Integrate removed the worktree; caller-driven Remove must do that")
	}
	if !branchExists(proj, m.branchName("k2")) {
		t.Fatal("Integrate removed the branch; caller-driven Remove must do that")
	}
	if !isAncestorOf(proj, m.branchName("k2"), "HEAD") {
		t.Fatal("post-merge verification failed: branch tip not contained in HEAD")
	}

	m.Remove(proj, "k2")
	if branchExists(proj, m.branchName("k2")) {
		t.Fatal("Remove did not tear down the branch")
	}
}

func TestIsAncestorOf(t *testing.T) {
	proj := gitInitRepo(t)
	base := head(t, proj)
	// Advance HEAD so base and HEAD are genuinely different commits.
	if err := os.WriteFile(filepath.Join(proj, "second.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, proj, "add", "-A")
	gitDir(t, proj, "commit", "-m", "second")
	if !isAncestorOf(proj, base, "HEAD") {
		t.Fatal("base should be ancestor of HEAD")
	}
	if isAncestorOf(proj, "HEAD", base) {
		t.Fatal("HEAD should not be ancestor of base")
	}
	if !isAncestorOf(proj, "HEAD", "HEAD") {
		t.Fatal("a ref is its own ancestor (equality must pass)")
	}
	if isAncestorOf(proj, "", "HEAD") {
		t.Fatal("empty ancestor must be false")
	}
	if isAncestorOf(proj, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "HEAD") {
		t.Fatal("unknown sha must be false")
	}
}

func gitDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func head(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitDir(t, dir, "rev-parse", "HEAD"))
}
