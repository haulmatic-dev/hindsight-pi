package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED (TDD) tests for worktree scratch scaffolding + ignore injection +
// lifecycle pruning.
//
// Contract pinned from the parent acceptance criteria:
//   - EnsureWorktree scaffolds a scratch directory for the chain key, located
//     OUTSIDE the worktree tree (never inside <worktree>/), and injects a
//     `.scratch/` ignore line into the worktree's .gitignore so scratch-
//     relative writes never leak into git status.
//   - Every teardown path (Remove, QuarantineBranchAndRemove,
//     CleanupStaleWorktrees) prunes the scratch dir — nothing is left behind.
//   - The worker env builder exports the scratch dir as HYDRA_SCRATCH_DIR so
//     tools like mongodb-memory-server (MONGOMS_TMPDIR-style) write their
//     scratch artifacts where teardown pruning can find them.
//
// The feature is NOT yet implemented (RED phase): the behavior tests skip
// loudly by default so the suite stays green pre-GREEN, and fail with the
// missing-feature reason under SCRATCH_ENFORCE=1.

// scratchRequireImpl gates scratch behavior tests on the feature. Skips
// loudly pre-GREEN ("RED: scratch scaffolding contract not yet implemented");
// SCRATCH_ENFORCE=1 fails instead so the RED phase provably detects the
// missing feature.
func scratchRequireImpl(t *testing.T) {
	t.Helper()
	if scratchScaffoldingImplemented {
		return
	}
	const msg = "RED: scratch scaffolding contract not yet implemented (worktree scratch dir + .gitignore injection + teardown pruning + HYDRA_SCRATCH_DIR export)"
	if os.Getenv("SCRATCH_ENFORCE") == "1" {
		t.Fatalf("%s", msg)
	}
	t.Skip(msg)
}

// TestScratch_DirCreatedOutsideTree: worktree creation scaffolds the scratch
// dir for the chain key, outside the worktree tree, with the `.scratch/`
// ignore line injected into the worktree .gitignore.
func TestScratch_DirCreatedOutsideTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "scratch-create-1"

	wt, err := m.EnsureWorktree(proj, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	scratch := worktreeScratchDirFor(proj, key)
	scratchRequireImpl(t)

	fi, err := os.Stat(scratch)
	if err != nil {
		t.Fatalf("scratch dir %s not created by EnsureWorktree: %v — worktree creation must scaffold a scratch dir outside the worktree tree", scratch, err)
	}
	if !fi.IsDir() {
		t.Fatalf("scratch path %s is not a directory (mode %v)", scratch, fi.Mode())
	}

	// Outside the worktree tree: scratch must never live inside the worktree.
	if scratch == wt || strings.HasPrefix(scratch, wt+string(os.PathSeparator)) {
		t.Fatalf("scratch dir %s must live OUTSIDE the worktree %s", scratch, wt)
	}

	// Ignore injection: the worktree .gitignore carries the literal `.scratch/`
	// line so scratch-relative writes never pollute git status.
	data, err := os.ReadFile(filepath.Join(wt, ".gitignore"))
	if err != nil {
		t.Fatalf("read worktree .gitignore: %v", err)
	}
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == ".scratch/" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("worktree .gitignore missing literal line %q (ignore injection); got:\n%s", ".scratch/", string(data))
	}
}

// requireScaffoldedScratch asserts the scratch dir for (proj, key) exists
// after EnsureWorktree — the precondition every prune-site test needs before
// teardown.
func requireScaffoldedScratch(t *testing.T, m *WorktreeManager, proj, key string) string {
	t.Helper()
	scratchRequireImpl(t)
	scratch := worktreeScratchDirFor(proj, key)
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("precondition: scratch dir %s must exist before teardown (EnsureWorktree must scaffold it): %v", scratch, err)
	}
	return scratch
}

// TestScratch_PruneSites: every teardown path removes the scratch dir and
// leaves nothing behind.
func TestScratch_PruneSites(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// ageWorktree backdates every file/dir under the worktree so the GC's
	// staleness window sees it as stale while the scratch dir stays fresh —
	// proving the scratch prune is explicit (tied to worktree teardown), not
	// accidental junk-dir cleanup.
	ageWorktree := func(t *testing.T, wt string, old time.Time) {
		t.Helper()
		err := filepath.WalkDir(wt, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chtimes(path, old, old)
		})
		if err != nil {
			t.Fatalf("backdate worktree %s: %v", wt, err)
		}
	}

	t.Run("Remove", func(t *testing.T) {
		proj := gitInitRepo(t)
		m := NewWorktreeManager()
		key := "scratch-prune-remove"
		if _, err := m.EnsureWorktree(proj, key); err != nil {
			t.Fatalf("EnsureWorktree: %v", err)
		}
		scratch := requireScaffoldedScratch(t, m, proj, key)
		m.Remove(proj, key)
		if _, err := os.Stat(scratch); !os.IsNotExist(err) {
			t.Fatalf("Remove left the scratch dir behind at %s (stat err %v) — teardown must prune the scratch dir", scratch, err)
		}
	})

	t.Run("QuarantineBranchAndRemove", func(t *testing.T) {
		proj := gitInitRepo(t)
		m := NewWorktreeManager()
		key := "scratch-prune-quarantine"
		if _, err := m.EnsureWorktree(proj, key); err != nil {
			t.Fatalf("EnsureWorktree: %v", err)
		}
		scratch := requireScaffoldedScratch(t, m, proj, key)
		if _, err := m.QuarantineBranchAndRemove(proj, key, m.BranchName(key)); err != nil {
			t.Fatalf("QuarantineBranchAndRemove: %v", err)
		}
		if _, err := os.Stat(scratch); !os.IsNotExist(err) {
			t.Fatalf("QuarantineBranchAndRemove left the scratch dir behind at %s (stat err %v) — teardown must prune the scratch dir", scratch, err)
		}
	})

	t.Run("CleanupStaleWorktrees", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv("HYDRA_HOME", base) // GC scans $HYDRA_HOME/.hydra-worktrees — isolate from the host
		proj := gitInitRepo(t)
		m := NewWorktreeManager()
		key := "scratch-prune-gc"
		wt, err := m.EnsureWorktree(proj, key)
		if err != nil {
			t.Fatalf("EnsureWorktree: %v", err)
		}
		scratch := requireScaffoldedScratch(t, m, proj, key)
		ageWorktree(t, wt, time.Now().Add(-2*time.Hour))

		removed := CleanupStaleWorktrees(time.Minute, map[string]bool{}, nil)
		if removed < 1 {
			t.Fatalf("CleanupStaleWorktrees removed %d worktrees, want >=1 (setup failure, not the scratch contract)", removed)
		}
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Fatalf("stale worktree %s still present after GC (setup failure)", wt)
		}
		// The scratch dir is FRESH (not aged) — only an explicit prune tied to
		// the worktree teardown removes it; the junk-dir sweep must not be the
		// mechanism that passes this test.
		if _, err := os.Stat(scratch); !os.IsNotExist(err) {
			t.Fatalf("CleanupStaleWorktrees left the scratch dir behind at %s (stat err %v) — GC must prune the scratch dir with its worktree", scratch, err)
		}
	})
}

// TestScratch_EnvExported: the worker env builder output contains the scratch
// env var, pointing at the chain's scratch dir.
func TestScratch_EnvExported(t *testing.T) {
	proj := t.TempDir()                 // path-only contract; no git repo needed
	t.Setenv("HYDRA_HOME", t.TempDir()) // worktreeRoot derives from $HYDRA_HOME — isolate from the host
	bead := types.Bead{ID: "scratch-env-1", Project: "p", Title: "scratch env"}

	b := NewWorkerEnvBuilder(DefaultConfig())
	env := b.Build(bead, proj, "implement", 0)

	scratchRequireImpl(t)

	want := worktreeScratchDirFor(proj, string(bead.ID))

	// Match the exact computed entry rather than the first %s= line: the
	// env builder legitimately starts from the parent environment, so when
	// the worker process itself already carries an inherited %s the slice
	// contains both — the builder appends the up-to-date computed value and
	// os/exec's duplicate handling keeps the last one.
	wantEntry := scratchEnvVar + "=" + want
	for _, e := range env {
		if e == wantEntry {
			return
		}
	}
	t.Fatalf("worker env missing %s — env builder must export the worktree scratch dir (tools like mongodb-memory-server write scratch artifacts via MONGOMS_TMPDIR-style paths)", scratchEnvVar)
}
