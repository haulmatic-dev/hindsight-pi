package core

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
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
func PublishRepo(m *WorktreeManager, projectPath, token string, activeKeys map[string]bool, isTerminal func(key string) bool) (bool, string) {
	if m == nil || projectPath == "" {
		return false, "no worktree manager"
	}
	// 1. Land any completed worktree work on the working branch.
	ReconcileOrphanedWorktrees(m, projectPath, activeKeys, isTerminal)

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
		if strings.HasPrefix(reason, "remote at") {
			log.Printf("[Publish] push verification FAILED for %s/%s: %s", projectPath, branch, reason)
		}
		log.Printf("[Publish] push skipped for %s: %s", projectPath, reason)
		AppendActivity(projectPath, "publish", "push skipped: "+reason)
	}
	return pushed, reason
}

// AutoPR exec-layer hooks. Overridden in tests to avoid real git/gh calls.
var (
	autoPRGitPush  = defaultAutoPRGitPush
	autoPRLsRemote = defaultAutoPRLsRemote
	autoPRGhCreate = defaultAutoPRGhCreate
)

// maybeOpenAutoPR creates a PR when a push to the protected base branch was
// rejected. It is fail-open: any error is logged and the integration result is
// left unchanged.
func maybeOpenAutoPR(beadsClient BeadsClient, cfg *Config, projectPath, repoName, branch string, bead *types.Bead) {
	if cfg == nil || !cfg.AutoPREnabled() || beadsClient == nil || bead == nil || branch == "" {
		return
	}
	head := "dogfood/" + string(bead.ID)
	for _, l := range bead.Labels {
		if l == "pr-open" {
			log.Printf("[AutoPR] skipping %s: already has pr-open label", bead.ID)
			return
		}
	}
	if out, err := autoPRLsRemote(projectPath, "origin", "refs/heads/"+head); err == nil && strings.TrimSpace(out) != "" {
		log.Printf("[AutoPR] skipping %s: %s already exists on origin", bead.ID, head)
		return
	}
	token := autoPRResolveToken()
	if err := autoPRGitPush(projectPath, "refs/heads/"+head, token); err != nil {
		log.Printf("[AutoPR] failed for %s: push %s: %v", bead.ID, head, err)
		return
	}
	title := bead.Title
	if title == "" {
		title = "Hydra auto-PR"
	}
	body := "Authored by a Hydra dogfood run."
	url, err := autoPRGhCreate(projectPath, branch, head, title, body, token)
	if err != nil {
		log.Printf("[AutoPR] failed for %s: %v", bead.ID, err)
		return
	}
	newLabels := make([]string, 0, len(bead.Labels)+1)
	hasOpen := false
	for _, l := range bead.Labels {
		if l == "pr-open" {
			hasOpen = true
		}
		newLabels = append(newLabels, l)
	}
	if !hasOpen {
		newLabels = append(newLabels, "pr-open")
	}
	if err := beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, newLabels); err != nil {
		log.Printf("[AutoPR] failed for %s: update labels: %v (fail open)", bead.ID, err)
	}
	desc := bead.Description
	if desc != "" {
		desc += "\n\n"
	}
	desc += "Auto-PR: " + url
	if err := beadsClient.UpdateBeadDescription(projectPath, repoName, bead.ID, desc); err != nil {
		log.Printf("[AutoPR] failed for %s: update description: %v (fail open)", bead.ID, err)
	}
	log.Printf("[Output] auto-PR created: %s", url)
}

// autoPRResolveToken prefers GH_TOKEN, then parses x-access-token from the
// ~/.git-credentials store used by the git push path.
func autoPRResolveToken() string {
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".git-credentials"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		idx := strings.Index(line, "x-access-token:")
		if idx < 0 {
			continue
		}
		rest := line[idx+len("x-access-token:"):]
		end := strings.IndexAny(rest, "@ \t")
		if end >= 0 {
			return rest[:end]
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

func defaultAutoPRGitPush(repoPath, ref, token string) error {
	originURL, err := gitOut(repoPath, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("get origin: %w", err)
	}
	target := authenticatedURL(strings.TrimSpace(originURL), token)
	if target == "" {
		target = "origin"
	}
	if _, err := gitOut(repoPath, "push", target, "HEAD:"+ref); err != nil {
		return fmt.Errorf("push HEAD:%s: %w", ref, err)
	}
	return nil
}

func defaultAutoPRLsRemote(repoPath, remote, ref string) (string, error) {
	return gitOut(repoPath, "ls-remote", remote, ref)
}

func defaultAutoPRGhCreate(repoPath, base, head, title, body, token string) (string, error) {
	cmd := exec.Command("gh", "pr", "create",
		"--base", base,
		"--head", head,
		"--title", title,
		"--body", body,
	)
	cmd.Dir = repoPath
	if token != "" {
		cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "http://") || strings.HasPrefix(lines[i], "https://") {
			return strings.TrimSpace(lines[i]), nil
		}
	}
	return "", fmt.Errorf("no PR URL in gh output: %s", strings.TrimSpace(string(out)))
}
