package core

import (
	"log"
	"strings"
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
func ReconcileOrphanedWorktrees(m *WorktreeManager, projectPath string, activeKeys map[string]bool, isTerminal func(key string) bool) (integrated int, conflicts int) {
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

		key := strings.TrimPrefix(branch, "hydra/wt-")
		if activeKeys[key] {
			continue
		}
		if isTerminal == nil || !isTerminal(key) {
			continue // live, resumable, or unverifiable work: never touch
		}

		if err := m.IntegrateAndRemove(projectPath, key); err != nil {
			// Terminal + conflict: preserve the work on a quarantine branch
			// and consume the wt branch — the alternative is retrying the
			// same failing merge every sweep forever.
			if qb, qerr := m.QuarantineAndRemove(projectPath, key); qerr != nil {
				log.Printf("[WorktreeReconcile] %s: integrate failed (%v); quarantine failed (%v)", key, err, qerr)
			} else {
				log.Printf("[WorktreeReconcile] %s: integrate conflicted — quarantined to %s (work preserved, retry loop ends)", key, qb)
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
