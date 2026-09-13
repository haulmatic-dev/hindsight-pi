package core

// Tests for the WorktreeGC safety gates that prevent reaping in-progress work
// (hydra-q3p was reaped mid-merge on a dep-parked bead, 2026-09-12 — merge
// resolution lost). GC must skip worktrees that are (a) non-clean per git
// status, (b) touched within the staleness window, or (c) on a branch with
// unintegrated commits whose bead is parked (not terminal).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCleanupStaleWorktrees_PreservesMidMerge: a stale-looking worktree with a
// merge in progress (conflicts not yet resolved) holds unmerged index entries
// — non-clean per git status — and must survive the sweep.
func TestCleanupStaleWorktrees_PreservesMidMerge(t *testing.T) {
	proj := gitInitRepo(t)
	t.Setenv("HYDRA_HOME", t.TempDir())
	m := NewWorktreeManager()

	seed := filepath.Join(proj, "seed.txt")
	if err := os.WriteFile(seed, []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, proj, "add", "-A")
	gitDir(t, proj, "commit", "-m", "base")
	gitDir(t, proj, "checkout", "-b", "other")
	if err := os.WriteFile(seed, []byte("other line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, proj, "commit", "-am", "other change")
	gitDir(t, proj, "checkout", "-")

	wt, err := m.EnsureWorktree(proj, "bead-m")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "seed.txt"), []byte("wt line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, wt, "commit", "-am", "wt change")
	if out, err := gitOut(wt, "merge", "other"); err == nil {
		t.Fatalf("expected merge conflict, merge succeeded: %s", out)
	}
	if st := gitDir(t, wt, "status", "--porcelain"); !strings.Contains(st, "UU") {
		t.Fatalf("expected unmerged entries, got: %q", st)
	}

	// Age the worktree past the GC horizon (as the incident worktree was).
	old := timeNowMinus48h()
	if err := os.Chtimes(filepath.Join(wt, ".git"), old, old); err != nil {
		t.Fatal(err)
	}

	removed := CleanupStaleWorktrees(24*time.Hour, map[string]bool{}, nil)
	if _, err := os.Stat(wt); os.IsNotExist(err) {
		t.Fatalf("mid-merge worktree was GC'd (removed=%d)", removed)
	}
}

// TestCleanupStaleWorktrees_PreservesRecentActivity: the .git pointer file's
// mtime is creation-time only; a worktree whose files were touched within the
// staleness window (merge-resolution edits, untracked worker output the dirty
// check deliberately ignores) is live work and must survive even when tracked
// status is clean.
func TestCleanupStaleWorktrees_PreservesRecentActivity(t *testing.T) {
	proj := gitInitRepo(t)
	t.Setenv("HYDRA_HOME", t.TempDir())
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "bead-r")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// Untracked, recently-written output; tracked status stays clean, so the
	// dirty check alone would pass this worktree to the reaper.
	if err := os.WriteFile(filepath.Join(wt, "resolution-notes.md"), []byte("merging...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := timeNowMinus48h()
	if err := os.Chtimes(filepath.Join(wt, ".git"), old, old); err != nil {
		t.Fatal(err)
	}

	eventsDir := t.TempDir()
	removed := CleanupStaleWorktrees(24*time.Hour, map[string]bool{}, NewEventSink(eventsDir))
	if removed != 0 {
		t.Fatalf("recently-active worktree must be preserved, removed=%d", removed)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("recently-active worktree dir removed: %v", err)
	}

	// Typed event recorded in the central stream.
	entries, _ := os.ReadDir(eventsDir)
	found := false
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(eventsDir, e.Name()))
		if strings.Contains(string(data), `"cause":"recent_activity"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("recent_activity preservation event missing from central stream (entries=%d)", len(entries))
	}
}
