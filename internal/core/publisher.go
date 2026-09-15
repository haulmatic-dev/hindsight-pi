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
//     branch is actually ahead of the remote (avoids no-op pushes each tick);
//     when prMerge is set (permanent branch protection on the working
//     branch) step 2 routes through PublishViaPR instead of a direct push.
//
// Fail-open and SILENT: never errors/panics; returns (pushed, reason). A
// missing worktree manager or empty branch is a no-op. The token is the
// project owner's PAT so commits are attributed to and pushed as that user.
func PublishRepo(m *WorktreeManager, projectPath, token string, prMerge bool, activeKeys map[string]bool, isTerminal func(key string) bool) (bool, string) {
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
	if prMerge {
		feature := "dogfood/publish-" + prSafeName(filepath.Base(projectPath))
		url, err := PublishViaPR(projectPath, branch, feature, "Hydra publish: "+branch, token)
		if err != nil {
			log.Printf("[Publish] PR merge failed for %s/%s: %v", projectPath, branch, err)
			AppendActivity(projectPath, "publish", "PR merge failed: "+err.Error())
			return false, "pr merge failed: " + err.Error()
		}
		log.Printf("[Publish] landed via PR %s for %s (%s)", url, projectPath, branch)
		AppendActivity(projectPath, "publish", "landed via PR "+url)
		return true, ""
	}
	pushed, reason := PushBranchWithToken(projectPath, branch, token)
	if pushed {
		log.Printf("[Publish] pushed %s (%s) to origin", projectPath, branch)
		AppendActivity(projectPath, "publish", "pushed "+branch)
	} else {
		if strings.HasPrefix(reason, "remote at") {
			log.Printf("[Publish] push verification FAILED for %s/%s: %s", projectPath, branch, reason)
		}
		if IsProtectedBranchRejection(reason) {
			log.Printf("[Publish] push rejected by branch protection for %s/%s: %s", projectPath, branch, reason)
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

// PR-merge exec-layer hooks. Overridden in tests to avoid real git/gh calls.
var (
	prMergeGitPush = defaultPRMergeGitPush
	autoPRGhMerge  = defaultAutoPRGhMerge
)

// PublishViaPR lands HEAD onto a protected base branch without a direct push:
// force-push HEAD to the dogfood feature branch, open a PR into base, merge
// it with `gh pr merge --merge --admin`, and verify the PR actually reached
// MERGED state (gh pr view) before reporting success. Fail-closed: any step
// error is returned with the URL of a created-but-unmerged PR so callers can
// surface it; a caller must not treat the integrate as landed on error.
// Uses a true merge commit (not squash) so local and remote histories stay
// convergent across repeated integrate cycles — squash merges reintroduce
// divergence on every bead. Branch protection must allow admin merges.
func PublishViaPR(projectPath, base, featureBranch, title, token string) (string, error) {
	if projectPath == "" || base == "" || featureBranch == "" {
		return "", fmt.Errorf("pr merge: empty project, base, or feature branch")
	}
	if err := prMergeGitPush(projectPath, "refs/heads/"+featureBranch, token); err != nil {
		return "", fmt.Errorf("pr merge: push %s: %w", featureBranch, err)
	}
	if title == "" {
		title = "Hydra dogfood PR"
	}
	url, err := autoPRGhCreate(projectPath, base, featureBranch, title, "Authored by a Hydra dogfood run.", token)
	if err != nil {
		return "", fmt.Errorf("pr merge: create PR: %w", err)
	}
	if err := autoPRGhMerge(projectPath, featureBranch, token); err != nil {
		return url, fmt.Errorf("pr merge: merge %s: %w", url, err)
	}
	return url, nil
}

func defaultPRMergeGitPush(repoPath, ref, token string) error {
	originURL, err := gitOut(repoPath, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("get origin: %w", err)
	}
	target := authenticatedURL(strings.TrimSpace(originURL), token)
	if target == "" {
		target = "origin"
	}
	// Force: the dogfood branch is hydra-owned per-id, and a retry after a
	// failed merge must be able to move it.
	if _, err := gitOut(repoPath, "push", "--force", target, "HEAD:"+ref); err != nil {
		return fmt.Errorf("push HEAD:%s: %w", ref, err)
	}
	return nil
}

// defaultAutoPRGhMerge merges the PR for head with a true merge commit,
// bypassing review requirements with --admin (branch protection on the base
// is permanent, so review-gated merges would stall every close). Fail-closed:
// a zero exit alone is not proof the merge landed — only MERGED state from
// `gh pr view` is.
func defaultAutoPRGhMerge(repoPath, head, token string) error {
	env := os.Environ()
	if token != "" {
		env = append(env, "GH_TOKEN="+token)
	}
	merge := exec.Command("gh", "pr", "merge", head, "--merge", "--admin")
	merge.Dir = repoPath
	merge.Env = env
	if out, err := merge.CombinedOutput(); err != nil {
		return fmt.Errorf("gh pr merge: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	view := exec.Command("gh", "pr", "view", head, "--json", "state", "--jq", ".state")
	view.Dir = repoPath
	view.Env = env
	out, err := view.Output()
	if err != nil {
		return fmt.Errorf("gh pr view: %w", err)
	}
	if state := strings.TrimSpace(string(out)); state != "MERGED" {
		return fmt.Errorf("gh pr view: state=%q, want MERGED", state)
	}
	return nil
}

// prSafeName reduces a project directory name to a branch-safe token.
func prSafeName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return '-'
	}, s)
}

// maybeOpenAutoPR creates a PR when a push to the protected base branch was
// rejected. It is fail-open: any error is logged and false is returned (the
// caller then fails the integration closed). Returns true only when the PR was
// actually created — the caller may then treat the integration as preserved
// via the PR.
func maybeOpenAutoPR(beadsClient BeadsClient, cfg *Config, projectPath, repoName, branch string, bead *types.Bead) bool {
	if cfg == nil || !cfg.AutoPREnabled() || beadsClient == nil || bead == nil || branch == "" {
		return false
	}
	head := "dogfood/" + string(bead.ID)
	for _, l := range bead.Labels {
		if l == "pr-open" {
			log.Printf("[AutoPR] skipping %s: already has pr-open label", bead.ID)
			return false
		}
	}
	if out, err := autoPRLsRemote(projectPath, "origin", "refs/heads/"+head); err == nil && strings.TrimSpace(out) != "" {
		log.Printf("[AutoPR] skipping %s: %s already exists on origin", bead.ID, head)
		return false
	}
	token := autoPRResolveToken()
	if err := autoPRGitPush(projectPath, "refs/heads/"+head, token); err != nil {
		log.Printf("[AutoPR] failed for %s: push %s: %v", bead.ID, head, err)
		return false
	}
	title := bead.Title
	if title == "" {
		title = "Hydra auto-PR"
	}
	body := "Authored by a Hydra dogfood run."
	url, err := autoPRGhCreate(projectPath, branch, head, title, body, token)
	if err != nil {
		log.Printf("[AutoPR] failed for %s: %v", bead.ID, err)
		return false
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
	return true
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
	// --no-verify: same rationale as PushBranch — client-side hooks are
	// interactive-dev affordances that break in headless daemon checkouts.
	if _, err := gitOut(repoPath, "push", "--no-verify", target, "HEAD:"+ref); err != nil {
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
