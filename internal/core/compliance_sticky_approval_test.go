package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func timeNowMinus48h() time.Time { return time.Now().Add(-48 * time.Hour) }

// TestComplianceCycle_StickyApproval verifies that once a PRD is approved the
// cycle never re-enters: previously checkAndRunCycle re-ran the analyzer every
// poll after approval, burned max_cycles, and falsely alarmed "manual
// intervention required".
func TestComplianceCycle_StickyApproval(t *testing.T) {
	dir := t.TempDir()
	cm := &ComplianceCycleManager{config: ComplianceCycleConfig{ProjectDir: dir}}

	if cm.isApproved() {
		t.Fatal("fresh manager must not be approved")
	}

	cm.markApproved()
	if !cm.isApproved() {
		t.Fatal("markApproved must set approval")
	}
	if _, err := os.Stat(filepath.Join(dir, ".hydra", "compliance-approved")); err != nil {
		t.Fatalf("sentinel file missing: %v", err)
	}

	// Restart durability: a NEW manager over the same project dir sees the
	// sentinel even with a cold in-memory flag.
	cm2 := &ComplianceCycleManager{config: ComplianceCycleConfig{ProjectDir: dir}}
	if !cm2.isApproved() {
		t.Fatal("approval must survive across manager instances (sentinel)")
	}

	// Guard ordering: with approval set, checkAndRunCycle returns nil before
	// touching the overseer (nil here — would panic if the guard ran later).
	if err := cm2.checkAndRunCycle(); err != nil {
		t.Fatalf("approved cycle must no-op, got: %v", err)
	}
}

// TestCleanupStaleWorktrees verifies the GC removes old inactive worktrees,
// keeps active and fresh ones, and clears non-worktree junk dirs.
func TestCleanupStaleWorktrees(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HYDRA_HOME", base)
	root := filepath.Join(base, ".hydra-worktrees", "proj")

	mk := func(key, gitContent string, old bool) string {
		wt := filepath.Join(root, key)
		if err := os.MkdirAll(wt, 0o755); err != nil {
			t.Fatal(err)
		}
		if gitContent != "" {
			gf := filepath.Join(wt, ".git")
			if err := os.WriteFile(gf, []byte(gitContent), 0o644); err != nil {
				t.Fatal(err)
			}
			if old {
				oldTime := timeNowMinus48h()
				if err := os.Chtimes(gf, oldTime, oldTime); err != nil {
					t.Fatal(err)
				}
			}
		} else if old {
			oldTime := timeNowMinus48h()
			if err := os.Chtimes(wt, oldTime, oldTime); err != nil {
				t.Fatal(err)
			}
		}
		return wt
	}

	stale := mk("bead-old", "gitdir: /nonexistent/.git/worktrees/bead-old\n", true)
	fresh := mk("bead-fresh", "gitdir: /nonexistent/.git/worktrees/bead-fresh\n", false)
	active := mk("bead-active", "gitdir: /nonexistent/.git/worktrees/bead-active\n", true)
	junk := mk("junk", "", true)

	removed := CleanupStaleWorktrees(24*time.Hour, map[string]bool{"bead-active": true}, nil)

	if removed != 2 {
		t.Errorf("expected 2 removed (stale + junk), got %d", removed)
	}
	for path, wantGone := range map[string]bool{stale: true, junk: true, fresh: false, active: false} {
		_, err := os.Stat(path)
		gone := os.IsNotExist(err)
		if gone != wantGone {
			t.Errorf("%s: gone=%v, want %v", filepath.Base(path), gone, wantGone)
		}
	}
}

// TestCleanupStaleWorktrees_PreservesUnintegrated: the GC must never wipe a
// stale worktree whose branch carries commits not yet in the working branch
// (48bdq lost its slice twice between verdict and verified-close,
// 2026-09-06/07). Preservation emits a typed worktree-preserved-unintegrated
// event instead of silently skipping.
func TestCleanupStaleWorktrees_PreservesUnintegrated(t *testing.T) {
	proj := gitInitRepo(t)
	home := t.TempDir()
	t.Setenv("HYDRA_HOME", home)
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "bead-x")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// Worker commits its slice; the branch tip is NOT contained in HEAD.
	if err := os.WriteFile(filepath.Join(wt, "spec.txt"), []byte("12 specs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, wt, "add", "-A")
	gitDir(t, wt, "commit", "-m", "worker slice")

	// Age the worktree past the GC horizon.
	gf := filepath.Join(wt, ".git")
	old := timeNowMinus48h()
	if err := os.Chtimes(gf, old, old); err != nil {
		t.Fatal(err)
	}

	eventsDir := t.TempDir()
	removed := CleanupStaleWorktrees(24*time.Hour, map[string]bool{}, NewEventSink(eventsDir))

	if removed != 0 {
		t.Fatalf("unintegrated worktree must be preserved, removed=%d", removed)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree dir removed despite unintegrated commits: %v", err)
	}
	if got := head(t, wt); got != strings.TrimSpace(gitDir(t, proj, "rev-parse", m.branchName("bead-x"))) {
		t.Fatalf("branch tip moved/lost: worktree head=%s", got)
	}

	// Typed event recorded in the central stream.
	entries, _ := os.ReadDir(eventsDir)
	found := false
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(eventsDir, e.Name()))
		if strings.Contains(string(data), `"type":"worktree-preserved-unintegrated"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("worktree-preserved-unintegrated event missing from central stream (entries=%d)", len(entries))
	}
}

// TestCleanupStaleWorktrees_PreservesDirtyTracked: uncommitted tracked
// changes are work too — GC must not destroy them.
func TestCleanupStaleWorktrees_PreservesDirtyTracked(t *testing.T) {
	proj := gitInitRepo(t)
	t.Setenv("HYDRA_HOME", t.TempDir())
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "bead-y")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// Modify a TRACKED file without committing (untracked node_modules-style
	// junk must NOT trigger preservation — untracked files are ignored).
	if err := os.WriteFile(filepath.Join(wt, "seed.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := timeNowMinus48h()
	if err := os.Chtimes(filepath.Join(wt, ".git"), old, old); err != nil {
		t.Fatal(err)
	}

	removed := CleanupStaleWorktrees(24*time.Hour, map[string]bool{}, nil)
	if removed != 0 {
		t.Fatalf("dirty worktree must be preserved, removed=%d", removed)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("dirty worktree dir removed: %v", err)
	}
}

// TestCleanupStaleWorktrees_RemovesCleanIntegrated: a stale worktree sitting
// at the working-branch tip with no new commits and no uncommitted tracked
// changes is pure leak — normal cleanup must still remove it (no regression).
func TestCleanupStaleWorktrees_RemovesCleanIntegrated(t *testing.T) {
	proj := gitInitRepo(t)
	t.Setenv("HYDRA_HOME", t.TempDir())
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "bead-z")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// Untracked junk alone must not block GC (node_modules case).
	if err := os.MkdirAll(filepath.Join(wt, "node_modules", "somepkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := timeNowMinus48h()
	// Backdate the whole tree, not just the .git pointer: production-stale
	// worktrees are uniformly old (creation backdates every checkout file),
	// and the activity gate compares file mtimes against creation time.
	if err := os.Chtimes(filepath.Join(wt, ".git"), old, old); err != nil {
		t.Fatal(err)
	}
	filepath.WalkDir(wt, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if path == filepath.Join(wt, ".git") {
			return nil
		}
		_ = os.Chtimes(path, old, old)
		return nil
	})

	removed := CleanupStaleWorktrees(24*time.Hour, map[string]bool{}, nil)
	if removed != 1 {
		t.Fatalf("clean integrated worktree must be GC'd, removed=%d", removed)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree dir still present after GC")
	}
}

// TestWorktreeSourceRepo verifies source-repo recovery from a .git pointer.
func TestWorktreeSourceRepo(t *testing.T) {
	gf := filepath.Join(t.TempDir(), ".git")
	if err := os.WriteFile(gf, []byte("gitdir: /repos/proj/.git/worktrees/bead-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := worktreeSourceRepo(gf); got != "/repos/proj" {
		t.Errorf("worktreeSourceRepo = %q, want /repos/proj", got)
	}
}
