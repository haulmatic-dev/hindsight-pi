package core

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// ReconcileOrphanedWorktrees scans a project's `hydra/wt-*` branches and merges
// any branch whose key is not in activeKeys AND whose bead is terminal
// (closed/completed/failed) back into the working branch. Branches owned by
// active workers are skipped so live work is not disturbed; branches whose bead
// is still open/in_progress (or whose status cannot be determined) are skipped
// too — integrating incomplete work is worse than leaving it parked (observed
// live: a parked worker's live worktree and an open bead's 1,356-line worktree
// were both treated as orphans). A nil isTerminal predicate fails closed:
// nothing is touched.
//
// Merge conflicts on terminal beads are quarantined (hydra/quarantine/<key>)
// instead of retried forever — previously a conflicted branch re-failed every
// sweep indefinitely (observed live: 23 stale branches → "conflicts=23" every
// 10 minutes for weeks).
//
// Returns the number of successfully integrated branches and the number that
// conflicted. If projectPath is not a git repository, or the working branch
// has no hydra worktree branches, it returns (0, 0) without error.
// reconcileMinIdle is the age floor for the reconciler: a worktree whose
// contents changed within this window is treated as live and skipped.
// 2026-10-29 emergency raise 30m -> 6h: with a single-lane provider the fleet's
	// workers think for >30m between worktree writes; the reconciler was
	// integrating+removing their live (read-only-pending) worktrees at each
	// hourly sweep — 69 `[Registry] FATAL: worktree` worker deaths in one day.
	// Proper fix (registry-keyed liveness, not mtimes) is beaded separately.
	const reconcileMinIdle = 6 * time.Hour

func ReconcileOrphanedWorktrees(m *WorktreeManager, projectPath string, activeKeys map[string]bool, isTerminal func(key string) bool, events *EventSink) (integrated int, conflicts int) {
	// Not a git repo — nothing to reconcile.
	if _, err := gitOut(projectPath, "rev-parse", "--git-dir"); err != nil {
		return 0, 0
	}

	out, err := gitOut(projectPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/hydra/wt-*")
	if err != nil {
		return 0, 0
	}

	branches := strings.Split(strings.TrimSpace(out), "\n")
	for _, branch := range branches {
		if branch == "" {
			continue
		}

		// Versioned-key normalization (webapp-tc5 incident, 2026-09-16): a
		// silently versioned branch is hydra/wt-<key>-vN, so the raw suffix
		// here is "<key>-vN" — but the bead, the registry rows, the active-key
		// set, and the worktree DIRECTORY all still key it as <key>. Matching
		// isTerminal/activeKeys on the raw suffix misses every one of them and
		// the versioned worktree was skipped forever while its chain sat
		// completed. The merge itself still uses the real branch name.
		key := strings.TrimPrefix(branch, "hydra/wt-")
		if base, versioned := splitVersionedKey(key); versioned {
			key = base
		}
		if activeKeys[key] {
			continue
		}
		if isTerminal == nil || !isTerminal(key) {
			continue // live, resumable, or unverifiable work: never touch
		}

		// Age floor: a worktree with writes younger than reconcileMinIdle is
		// in active use — never integrate/remove it, regardless of what any
		// registry row, chain registration, or bead status claims. Defense in
		// depth after the 2026-09-16 live-sandbox deletion (webapp-zpf).
		if m.WorktreeRecentlyActive(projectPath, key, reconcileMinIdle) {
			continue
		}

		// Per-repo integration gate: same serialization as the close path —
		// the reconciler sweep merging several stale branches back-to-back is
		// itself a competitor for the base branch.
		if err := integrationGate(projectPath, func() error {
			return m.IntegrateBranchAndRemove(projectPath, key, branch)
		}); err != nil {
			// Terminal + conflict: preserve the work on a quarantine branch
			// and consume the wt branch — the alternative is retrying the
			// same failing merge every sweep forever.
			if qb, qerr := m.QuarantineBranchAndRemove(projectPath, key, branch); qerr != nil {
				log.Printf("[WorktreeReconcile] %s: integrate failed (%v); quarantine failed (%v)", key, err, qerr)
			} else {
				log.Printf("[WorktreeReconcile] %s: integrate conflicted — quarantined to %s (work preserved, retry loop ends)", key, qb)
				// Sensor symmetry (fire): the paired resolve (quarantine_restored)
				// is emitted by the retry-quarantine repair with the same
				// correlation ID when the quarantined branch merges clean again.
				events.EmitRepo(projectPath, Event{Type: "worktree_quarantined", Project: filepath.Base(projectPath),
					Bead: key, Reason: "merge_conflict",
					Detail: fmt.Sprintf("%s quarantined to %s after integrate conflict", branch, qb),
					Fields: map[string]any{"quarantine_branch": qb, "correlation_id": SensorCorrelation("worktree-reconcile", key)}})
			}
			conflicts++
			continue
		}
		integrated++
	}

	if integrated+conflicts > 0 {
		log.Printf("[WorktreeReconcile] project=%s integrated=%d conflicts=%d", projectPath, integrated, conflicts)
	}
	return integrated, conflicts
}
