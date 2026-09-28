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
		log.Printf("[Publish] skip %s: no working branch (err=%v)", projectPath, err)
		return false, "no branch"
	}
	// 2. Only push when there is something new to publish.
	if !branchAheadOfRemote(projectPath, branch) {
		log.Printf("[Publish] %s: branch %s matches origin, nothing to publish", projectPath, branch)
		return false, "up to date"
	}
	publishEvents.EmitRepo(projectPath, Event{Type: "publish_push_attempted", Project: filepath.Base(projectPath),
		Detail: "publisher attempting to land " + branch + " on origin",
		Fields: map[string]any{"branch": branch, "pr_merge": prMerge, "project_path": projectPath}})
	log.Printf("[Publish] push attempted for %s (%s) pr_merge=%v", projectPath, branch, prMerge)
	if prMerge {
		feature := "dogfood/publish-" + prSafeName(filepath.Base(projectPath))
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_pr_invoked", Project: filepath.Base(projectPath),
			Detail: "PublishViaPR invoked for " + branch + " via " + feature,
			Fields: map[string]any{"branch": branch, "feature_branch": feature, "project_path": projectPath}})
		log.Printf("[Publish] PublishViaPR invoked for %s/%s via %s", projectPath, branch, feature)
		url, err := PublishViaPR(projectPath, branch, feature, "Hydra publish: "+branch, token)
		if err != nil {
			publishEvents.EmitRepo(projectPath, Event{Type: "publish_push_rejected", Project: filepath.Base(projectPath), Reason: "pr_merge_failed",
				Detail: "PR merge failed for " + branch + ": " + err.Error(),
				Fields: map[string]any{"branch": branch, "feature_branch": feature, "project_path": projectPath}})
			log.Printf("[Publish] PR merge failed for %s/%s: %v", projectPath, branch, err)
			AppendActivity(projectPath, "publish", "PR merge failed: "+err.Error())
			return false, "pr merge failed: " + err.Error()
		}
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_pr_opened", Project: filepath.Base(projectPath),
			Detail: "landed via PR " + url,
			Fields: map[string]any{"branch": branch, "pr_url": url, "project_path": projectPath}})
		log.Printf("[Publish] landed via PR %s for %s (%s)", url, projectPath, branch)
		AppendActivity(projectPath, "publish", "landed via PR "+url)
		return true, ""
	}
	pushed, reason := PushBranchWithToken(projectPath, branch, token)
	if pushed {
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_push_succeeded", Project: filepath.Base(projectPath),
			Detail: "pushed " + branch + " to origin",
			Fields: map[string]any{"branch": branch, "project_path": projectPath}})
		log.Printf("[Publish] pushed %s (%s) to origin", projectPath, branch)
		AppendActivity(projectPath, "publish", "pushed "+branch)
	} else {
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_push_rejected", Project: filepath.Base(projectPath), Reason: "direct_push_failed",
			Detail: "push rejected/skipped for " + branch + ": " + reason,
			Fields: map[string]any{"branch": branch, "reason": reason, "project_path": projectPath}})
		log.Printf("[Publish] push rejected for %s/%s: %s", projectPath, branch, reason)
		if strings.HasPrefix(reason, "remote at") {
			log.Printf("[Publish] push verification FAILED for %s/%s: %s", projectPath, branch, reason)
		}
		if IsProtectedBranchRejection(reason) {
			log.Printf("[Publish] push rejected by branch protection for %s/%s: %s", projectPath, branch, reason)
		}
		AppendActivity(projectPath, "publish", "push skipped: "+reason)
	}
	return pushed, reason
}

// AutoPR exec-layer hooks. Overridden in tests to avoid real git/gh calls.
var (
	autoPRGitPush  = defaultAutoPRGitPush
	autoPRLsRemote = defaultAutoPRLsRemote
	autoPRGhCreate = defaultAutoPRGhCreate
	// autoPRGhListOpen reports whether an open PR already tracks the head
	// branch. Used to distinguish a stranded dogfood branch (pushed by an
	// earlier cycle, PR never created — hydra-f40k) from one already being
	// carried by an open PR.
	autoPRGhListOpen = defaultAutoPRGhListOpen
)

// publishEvents carries publisher telemetry into the typed event stream.
// Nil-safe (a nil *EventSink is a no-op on every method), wired once by the
// overseer at startup. The publisher stays fail-open: an event-write failure
// can never break a push.
var publishEvents *EventSink

// SetPublishEventSink wires the publisher's typed-event sink.
func SetPublishEventSink(s *EventSink) { publishEvents = s }

// PR-merge exec-layer hooks. Overridden in tests to avoid real git/gh calls.
var (
	prMergeGitPush = defaultPRMergeGitPush
	autoPRGhMerge  = defaultAutoPRGhMerge
)

// tipMergedProbe reports whether repoPath's HEAD is already contained in the
// remote base branch — i.e. a prior integrate/PR merge landed the same work.
// Overridden in tests.
var tipMergedProbe = defaultTipMergedProbe

// defaultTipMergedProbe fetches the remote base quietly and asks
// merge-base --is-ancestor. Fail-open: any git fault returns false so the
// caller proceeds with the normal publish path.
func defaultTipMergedProbe(repoPath, base string) bool {
	if _, err := gitOut(repoPath, "fetch", "--quiet", "origin", base); err != nil {
		return false
	}
	tip, err := gitOut(repoPath, "rev-parse", "HEAD")
	if err != nil {
		return false
	}
	return isAncestorOf(repoPath, strings.TrimSpace(tip), "origin/"+base)
}

// isPRNoCommitsBetween reports the benign duplicate-landing race: the winning
// close's PR merge landed the tip between the loser's pre-check and gh pr
// create, so GitHub rejects the PR with "No commits between <base> and
// <head>" (hydra-kf2t, 2026-09-27). The work is already on the base branch —
// treat it as landed.
func isPRNoCommitsBetween(err error) bool {
	return err != nil && strings.Contains(err.Error(), "No commits between")
}

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
	// failed merge must be able to move it. --no-verify: client-side pre-push
	// hooks (husky lint:ci && build) are interactive-dev affordances that
	// strand verified closes in headless daemon checkouts — the daemon already
	// ran its own gates (2026-09-27: 64 telematics commits stranded locally).
	// redactToken: the authenticated URL embeds the GitHub PAT and gitOut
	// error strings carry the full command — never let it reach logs.
	if _, err := gitOut(repoPath, "push", "--force", "--no-verify", target, "HEAD:"+ref); err != nil {
		return fmt.Errorf("push HEAD:%s: %s", ref, redactToken(err.Error(), token))
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
// caller then fails the integration closed). Returns true only when the work
// is verified landed — either the PR it created reached MERGED state, or an
// already-open PR tracking the dogfood branch was merged now. A leftover
// dogfood branch alone is NOT proof of preservation: an earlier cycle can
// have pushed it while PR creation never happened (the 2026-09-25 f40k
// incident stranded five verified closes exactly this way), and an open PR
// can have been left unmerged by its originator integration.
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
	token := autoPRResolveToken()
	if out, err := autoPRLsRemote(projectPath, "origin", "refs/heads/"+head); err == nil && strings.TrimSpace(out) != "" {
		open, err := autoPRGhListOpen(projectPath, head, token)
		if err != nil {
			log.Printf("[AutoPR] %s: listing PRs for %s: %v (fail open)", bead.ID, head, err)
			return false
		}
		if open {
			// The branch is already carried by an open PR — but the earlier
			// integration may never have merged it. Land it now; only a
			// verified MERGED state counts as preserved.
			if err := autoPRGhMerge(projectPath, head, token); err != nil {
				log.Printf("[AutoPR] %s: open PR for %s exists but merge failed: %v", bead.ID, head, err)
				return false
			}
			log.Printf("[Output] auto-PR merged for %s: %s", bead.ID, head)
			return true
		}
		log.Printf("[AutoPR] %s: %s exists on origin with no open PR — recovering stranded branch", bead.ID, head)
	}
	title := bead.Title
	if title == "" {
		title = "Hydra auto-PR"
	}
	// PublishViaPR force-updates the bead-owned dogfood branch, opens the PR,
	// merges with --admin, and verifies MERGED state — a created-but-unmerged
	// PR must not count as preserved (that was the f40k strand factory).
	url, err := PublishViaPR(projectPath, branch, head, title, token)
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

// defaultAutoPRGhListOpen reports whether at least one open PR tracks head.
// gh pr list --head matches by branch name; a non-zero exit fails open at the
// caller (the caller returns false = not preserved).
func defaultAutoPRGhListOpen(repoPath, head, token string) (bool, error) {
	cmd := exec.Command("gh", "pr", "list", "--head", head, "--state", "open", "--json", "number", "--jq", "length")
	cmd.Dir = repoPath
	if token != "" {
		cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	}
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("gh pr list: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return false, fmt.Errorf("gh pr list: parse count %q: %w", strings.TrimSpace(string(out)), err)
	}
	return n > 0, nil
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
		return fmt.Errorf("push HEAD:%s: %s", ref, redactToken(err.Error(), token))
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
