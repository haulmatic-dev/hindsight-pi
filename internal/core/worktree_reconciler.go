package core

import (
	"log"
	"strings"
)

// ReconcileOrphanedWorktrees scans a project's `hydra/wt-*` branches and merges
// any branch whose key is not in activeKeys back into the working branch.
// Branches owned by active workers are skipped so live work is not disturbed.
// Merge conflicts abort and the branch is left in place for inspection.
//
// Returns the number of successfully integrated branches and the number that
// could not be merged (conflicts or other errors). If projectPath is not a
// git repository, or the working branch has no hydra worktree branches, it
// returns (0, 0) without error.
func ReconcileOrphanedWorktrees(m *WorktreeManager, projectPath string, activeKeys map[string]bool) (integrated int, conflicts int) {
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

		if err := m.IntegrateAndRemove(projectPath, key); err != nil {
			log.Printf("[WorktreeReconcile] %s: %v", key, err)
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
