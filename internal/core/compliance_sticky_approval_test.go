package core

import (
	"os"
	"path/filepath"
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

	removed := CleanupStaleWorktrees(24*time.Hour, map[string]bool{"bead-active": true})

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
