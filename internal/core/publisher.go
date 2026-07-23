package core

import (
	"log"
	"strconv"
	"strings"
)

// branchAheadOfRemote reports whether localBranch has commits not yet on
// origin/localBranch. When origin/localBranch does not exist (never pushed) it
// returns true (there is something to publish). Any git error → false (nothing
// we can safely claim to publish).
func branchAheadOfRemote(repoPath, branch string) bool {
	if branch == "" {
		return false
	}
	// If the remote-tracking ref is missing, the branch has never been pushed.
	if _, err := gitOut(repoPath, "rev-parse", "--verify", "origin/"+branch); err != nil {
		return true
	}
	out, err := gitOut(repoPath, "rev-list", "--count", "origin/"+branch+".."+branch)
	if err != nil {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return false
	}
	return n > 0
}

// PublishRepo consolidates a project's completed work onto its working branch
// and pushes it to origin using the owner's token. It:
//  1. reconciles orphaned (completed, non-active) worktree branches into the
//     working branch, so per-bead micro-commits land there;
//  2. pushes the working branch to origin with the token, but only when the
//     branch is actually ahead of the remote (avoids no-op pushes each tick).
//
// Fail-open and SILENT: never errors/panics; returns (pushed, reason). A
// missing worktree manager or empty branch is a no-op. The token is the
// project owner's PAT so commits are attributed to and pushed as that user.
func PublishRepo(m *WorktreeManager, projectPath, token string, activeKeys map[string]bool) (bool, string) {
	if m == nil || projectPath == "" {
		return false, "no worktree manager"
	}
	// 1. Land any completed worktree work on the working branch.
	ReconcileOrphanedWorktrees(m, projectPath, activeKeys)

	branch, err := m.WorkingBranch(projectPath)
	if err != nil || branch == "" {
		return false, "no branch"
	}
	// 2. Only push when there is something new to publish.
	if !branchAheadOfRemote(projectPath, branch) {
		return false, "up to date"
	}
	pushed, reason := PushBranchWithToken(projectPath, branch, token)
	if pushed {
		log.Printf("[Publish] pushed %s (%s) to origin", projectPath, branch)
		AppendActivity(projectPath, "publish", "pushed "+branch)
	} else {
		log.Printf("[Publish] push skipped for %s: %s", projectPath, reason)
		AppendActivity(projectPath, "publish", "push skipped: "+reason)
	}
	return pushed, reason
}
