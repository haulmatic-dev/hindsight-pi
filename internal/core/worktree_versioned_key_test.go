package core

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Regression (2026-09-16 webapp-tc5 incident): a stale hydra/wt-<key> branch
// forced silent versioning to hydra/wt-<key>-v2. Nothing recorded the real
// branch name, so chain completion's exact-key lookup found no worktree and
// logged "SKIPPING integrate ... (nothing to integrate)" while 1,642
// insertions sat stranded. These tests pin the loud log, the recorded-key
// propagation, the -vN fallback order, and the reconciler backstop.

func TestEnsureWorktree_StaleBranchReattachLoggedLoudly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "webapp-tc5"

	// Stale residue: a wt branch with an unintegrated commit, no worktree —
	// the exact state that forced silent -v2 versioning in production
	// (webapp-tc5, 2026-09-16).
	base, _ := gitOut(projectPath, "rev-parse", "HEAD")
	staleBranch := m.branchName(key)
	if _, err := gitOut(projectPath, "branch", staleBranch, strings.TrimSpace(base)); err != nil {
		t.Fatalf("seed stale branch: %v", err)
	}
	seedDir := filepath.Join(t.TempDir(), "seed")
	if _, err := gitOut(projectPath, "worktree", "add", seedDir, staleBranch); err != nil {
		t.Fatalf("seed worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "stale.txt"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(seedDir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(seedDir, "commit", "--no-verify", "-m", "bead(stale): unmerged commit"); err != nil {
		t.Fatal(err)
	}
	staleTip, _ := gitOut(projectPath, "rev-parse", staleBranch)
	if _, err := gitOut(projectPath, "worktree", "remove", seedDir, "--force"); err != nil {
		t.Fatalf("remove seed worktree: %v", err)
	}
	if _, err := gitOut(projectPath, "worktree", "prune"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree with stale branch: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "downstream lookups must use the recorded key") {
		t.Fatalf("stale-branch reattach log missing recorded-key warning:\n%s", out)
	}

	// Reattach keeps the unintegrated commits on the SAME branch (the
	// af7v7 fix) — and ResolveBranch reports that real branch so the
	// recorded-key contract holds even if a future path versions the name.
	if got := m.ResolveBranch(projectPath, key); got != staleBranch {
		t.Fatalf("ResolveBranch = %q, want %q", got, staleBranch)
	}
	head, _ := gitOut(wt, "rev-parse", "HEAD")
	if strings.TrimSpace(head) != strings.TrimSpace(staleTip) {
		t.Fatalf("reattach rewound stale commits: %s vs %s", head, staleTip)
	}
}

func TestResolveChainBranch_FallbackOrder(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "webapp-tc5"

	// Nothing anywhere.
	if b := resolveChainBranch(m, projectPath, key, ""); b != "" {
		t.Fatalf("no branches: resolveChainBranch = %q, want empty", b)
	}

	// Only versioned branches: newest N wins, loudly.
	base, _ := gitOut(projectPath, "rev-parse", "HEAD")
	for _, suffix := range []string{"-v2", "-v3"} {
		if _, err := gitOut(projectPath, "branch", m.branchName(key)+suffix, strings.TrimSpace(base)); err != nil {
			t.Fatalf("seed %s: %v", suffix, err)
		}
	}
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	if b := resolveChainBranch(m, projectPath, key, ""); b != m.branchName(key)+"-v3" {
		t.Fatalf("newest -vN: resolveChainBranch = %q, want %q", b, m.branchName(key)+"-v3")
	}
	if !strings.Contains(buf.String(), "fell back") {
		t.Fatalf("fallback must be logged loudly:\n%s", buf.String())
	}
	buf.Reset()

	// Exact match beats -vN.
	if _, err := gitOut(projectPath, "branch", m.branchName(key), strings.TrimSpace(base)); err != nil {
		t.Fatal(err)
	}
	if b := resolveChainBranch(m, projectPath, key, ""); b != m.branchName(key) {
		t.Fatalf("exact beats -vN: resolveChainBranch = %q, want %q", b, m.branchName(key))
	}

	// Recorded branch beats everything, no fallback noise.
	if b := resolveChainBranch(m, projectPath, key, m.branchName(key)+"-v2"); b != m.branchName(key)+"-v2" {
		t.Fatalf("recorded branch wins: resolveChainBranch = %q", b)
	}
	if buf.Len() != 0 {
		t.Fatalf("recorded-key path must be silent, got:\n%s", buf.String())
	}
}

func TestReconcileOrphanedWorktrees_IntegratesVersionedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "webapp-tc5"

	// tc5 scenario: worktree dir <key> on branch hydra/wt-<key>-v2, chain done.
	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("tc5 work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wt, "bead(tc5): chain work"); err != nil {
		t.Fatal(err)
	}
	// Rename the branch to the versioned name, mimicking silent versioning.
	if _, err := gitOut(projectPath, "branch", "-m", m.branchName(key), m.branchName(key)+"-v2"); err != nil {
		t.Fatalf("version branch: %v", err)
	}
	backdateWorktree(t, m, projectPath, key)

	// isTerminal resolves by BASE key: the bead is webapp-tc5, not -v2.
	terminal := func(k string) bool { return k == key }
	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, terminal)
	if conflicts != 0 {
		t.Fatalf("conflicts = %d, want 0", conflicts)
	}
	if integrated != 1 {
		t.Fatalf("integrated = %d, want 1 (versioned worktree must not be skipped)", integrated)
	}
	if branchExists(projectPath, m.branchName(key)+"-v2") {
		t.Fatal("versioned branch should be consumed by integration")
	}
	if _, err := os.Stat(filepath.Join(projectPath, "feature.txt")); err != nil {
		t.Fatalf("work must land on the working branch: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree should be removed after integrate, stat err=%v", err)
	}
}

func TestReconcileOrphanedWorktrees_VersionedActiveKeyProtected(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "webapp-tc5"

	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if _, err := m.Commit(wt, "bead(tc5): in-flight"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(projectPath, "branch", "-m", m.branchName(key), m.branchName(key)+"-v2"); err != nil {
		t.Fatal(err)
	}
	backdateWorktree(t, m, projectPath, key)

	// The chain is actively being worked under the base key — even a
	// versioned branch must NOT be touched.
	active := map[string]bool{key: true}
	integrated, _ := ReconcileOrphanedWorktrees(m, projectPath, active, func(string) bool { return true })
	if integrated != 0 {
		t.Fatalf("integrated = %d, want 0 (active chain protected)", integrated)
	}
	if !branchExists(projectPath, m.branchName(key)+"-v2") {
		t.Fatal("versioned branch of an active chain must survive")
	}
}

func TestRecordChainWorktree_PropagatesToChainInfo(t *testing.T) {
	detector := NewChainCompletionDetector(ChainCompletionConfig{}, &Overseer{})
	root := types.BeadID("webapp-tc5")
	detector.RegisterChain(root, []types.BeadID{root, types.BeadID("webapp-tc5b")}, "/tmp/webapp")

	detector.RecordChainWorktree("webapp-tc5", "hydra/wt-webapp-tc5-v2")

	info, ok := detector.GetChainInfo(root)
	if !ok {
		t.Fatal("chain not registered")
	}
	if info.WorktreeKey != "webapp-tc5" {
		t.Errorf("WorktreeKey = %q, want webapp-tc5", info.WorktreeKey)
	}
	if info.WorktreeBranch != "hydra/wt-webapp-tc5-v2" {
		t.Errorf("WorktreeBranch = %q, want hydra/wt-webapp-tc5-v2", info.WorktreeBranch)
	}
}
