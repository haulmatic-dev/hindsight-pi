package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// prWaiting tracks branch tips that already have a PR standing open awaiting
// a human merge (tier policy). While the recorded tip is unchanged, publisher
// ticks skip the publish attempt entirely — re-pushing the same tip and
// re-attempting the same merge is pure churn and its events read as failures.
// Process-local: a daemon restart re-discovers the waiting PR once, which is
// cheap and self-healing.
var (
	prWaitingMu sync.Mutex
	prWaiting   = map[string]string{} // projectPath+"\x00"+branch -> tip sha
)

func prWaitingKey(projectPath, branch string) string { return projectPath + "\x00" + branch }

func prWaitingMark(projectPath, branch string) string {
	prWaitingMu.Lock()
	defer prWaitingMu.Unlock()
	return prWaiting[prWaitingKey(projectPath, branch)]
}

func setPRWaiting(projectPath, branch, tip string) {
	prWaitingMu.Lock()
	defer prWaitingMu.Unlock()
	prWaiting[prWaitingKey(projectPath, branch)] = tip
}

func clearPRWaiting(projectPath, branch string) {
	prWaitingMu.Lock()
	defer prWaitingMu.Unlock()
	delete(prWaiting, prWaitingKey(projectPath, branch))
}

// localBranchTip resolves a local branch tip; error → caller falls through to
// the normal publish path (fail-open).
func localBranchTip(projectPath, branch string) (string, error) {
	out, err := exec.Command("git", "-C", projectPath, "rev-parse", "refs/heads/"+branch).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

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

// branchMergedIntoBase reports whether the working branch's tip is already
// an ancestor of origin/<integration base> — i.e. the branch's work is fully
// merged upstream. Publishing it again can only produce a no-op PR (gh: "No
// commits between <base> and <head>") or, once the merged remote branch is
// pruned, the blank-sha GraphQL failure — the hydra-ybhh publish loop
// (2026-10-02: working branch dogfood/orphan-digest-escalated tip already
// merged via PR #144, remote branch deleted, publish retried every tick).
// Fail-open: resolution or git errors return false so the normal publish
// path runs.
func branchMergedIntoBase(projectPath, branch string) bool {
	if projectPath == "" || branch == "" {
		return false
	}
	base := originDefaultBranch(projectPath)
	if base == "" || base == branch {
		return false
	}
	// Ensure origin/<base> actually exists before trusting merge-base.
	if _, err := gitOut(projectPath, "rev-parse", "--verify", "origin/"+base); err != nil {
		return false
	}
	cmd := exec.Command("git", "-C", projectPath, "merge-base", "--is-ancestor", branch, "origin/"+base)
	if err := cmd.Run(); err != nil {
		// exit 1 = not an ancestor (expected); anything else is a git error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false
		}
		return false
	}
	return true
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
	ReconcileOrphanedWorktrees(m, projectPath, activeKeys, isTerminal, publishEvents)

	// 1b. Delete remote integration branches whose PRs have merged
	// (hydra-9dp5): without cleanup every campaign leaves one permanent
	// remote branch per bead — srv-telematics accumulated 88 in one
	// campaign. Fail-open: cleanup must never gate publishing.
	if token != "" {
		CleanupMergedIntegrationBranches(projectPath, token)
	}

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
	// 2b. Skip when the branch tip is already merged into the integration
	// base — re-publishing can only produce a no-op PR (hydra-ybhh loop).
	if branchMergedIntoBase(projectPath, branch) {
		log.Printf("[Publish] %s: branch %s tip already merged into origin base — skipping publish", projectPath, branch)
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_push_skipped_merged", Project: filepath.Base(projectPath),
			Detail: "branch " + branch + " tip already merged into origin integration base — no publish needed",
			Fields: map[string]any{"branch": branch, "project_path": projectPath}})
		AppendActivity(projectPath, "publish", "skipped publish: branch tip already merged into origin base")
		return false, "already integrated"
	}
	// 2c. Junk guard (hydra-ai97h): refuse to push branches whose committed
	// diff contains junk-signature paths. Fail closed with a bounded
	// ladder — after publishJunkMaxBlocks consecutive blocks the escalated
	// reason stops retry churn. Probe failures fail open and never block a
	// healthy publish.
	if publishJunkImplemented {
		if blocked, reason := publishJunkGuard(projectPath); blocked {
			publishEvents.EmitRepo(projectPath, Event{Type: publishJunkGuardRefusedEvent, Project: filepath.Base(projectPath),
				Reason: reason,
				Detail: "publish refused by junk guard",
				Fields: map[string]any{"reason": reason, "branch": branch, "project_path": projectPath}})
			AppendActivity(projectPath, "publish", "skipped publish: junk guard blocked ("+reason+")")
			return false, reason
		}
	}
	publishEvents.EmitRepo(projectPath, Event{Type: "publish_push_attempted", Project: filepath.Base(projectPath),
		Detail: "publisher attempting to land " + branch + " on origin",
		Fields: map[string]any{"branch": branch, "pr_merge": prMerge, "project_path": projectPath}})
	log.Printf("[Publish] push attempted for %s (%s) pr_merge=%v", projectPath, branch, prMerge)
	if prMerge {
		feature := IntegrationBranchPrefix(projectPath) + "publish-" + prSafeName(filepath.Base(projectPath))
		// Idempotence: when this project/branch already has a PR standing
		// open (tier policy: waiting on a human merge), re-invoking
		// PublishViaPR just re-pushes the same tip and re-attempts the same
		// merge — 114 publish_push_rejected events/day across 3 projects on
		// 2026-10-06, and the pr_merge_failed misnomer poisoned audit signal
		// (routine waiting read as failure). Skip while the same branch tip
		// is still ahead; a new bead landing on the branch flips
		// branchAheadOfRemote back to true with a NEW tip and re-publishes.
		if mark := prWaitingMark(projectPath, branch); mark != "" {
			if tip, terr := localBranchTip(projectPath, branch); terr == nil && tip == mark {
				return false, "pr awaiting human merge"
			}
			clearPRWaiting(projectPath, branch) // tip advanced since the wait was recorded
		}
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_pr_invoked", Project: filepath.Base(projectPath),
			Detail: "PublishViaPR invoked for " + branch + " via " + feature,
			Fields: map[string]any{"branch": branch, "feature_branch": feature, "project_path": projectPath}})
		log.Printf("[Publish] PublishViaPR invoked for %s/%s via %s", projectPath, branch, feature)
		url, err := publishWithRebaseRetry(
			func() (string, error) {
				return PublishViaPR(projectPath, branch, feature, "Hydra publish: "+branch, token)
			},
			func() error { return rebaseCheckoutOntoBase(projectPath, branch) },
			projectPath, branch)
		if err != nil {
			var pw *PRWaitingError
			if errors.As(err, &pw) {
				// Standing-open PR is the DESIGNED tier policy, not a failure:
				// emit publish_pr_waiting (reason pr_awaiting_human), record the
				// branch tip so later ticks skip without re-pushing, and do not
				// emit publish_push_rejected — that event is reserved for real
				// failures (credential-class, conflicts, API errors).
				if tip, terr := localBranchTip(projectPath, branch); terr == nil {
					setPRWaiting(projectPath, branch, tip)
				}
				publishEvents.EmitRepo(projectPath, Event{Type: "publish_pr_waiting", Project: filepath.Base(projectPath), Reason: "pr_awaiting_human",
					Detail: "PR left open awaiting human merge for " + branch + ": " + pw.URL,
					Fields: map[string]any{"branch": branch, "feature_branch": feature, "pr_url": pw.URL, "project_path": projectPath}})
				log.Printf("[Publish] %s/%s: PR awaiting human merge: %s", projectPath, branch, pw.URL)
				AppendActivity(projectPath, "publish", "PR awaiting human merge: "+pw.URL)
				return false, "pr awaiting human merge"
			}
			mergeErr := err.Error()
			cls := ClassifyPushFailure(err.Error())
			// NOT_FOUND is credential-class (org rename / missing PAT
			// scope): retrying against the same URL can never succeed.
			// Surface it loud instead of burying it as a generic merge
			// failure (hydra-m372: 91 not-found push events in one day
			// because the owner PAT predates the org rename).
			if cls == ClassNotFound {
				mergeErr += " — credential-class failure (repository not found with the publish token; org rename or missing PAT visibility/scope): human must fix the remote URL or the publish credential; retrying cannot succeed"
				log.Printf("[Publish] CREDENTIAL-CLASS publish failure for %s/%s: %v", projectPath, branch, err)
			}
			publishEvents.EmitRepo(projectPath, Event{Type: "publish_push_rejected", Project: filepath.Base(projectPath), Reason: "pr_merge_failed",
				Detail: "PR merge failed for " + branch + ": " + mergeErr,
				Fields: map[string]any{"branch": branch, "feature_branch": feature, "project_path": projectPath}})
			publishEvents.EmitRepo(projectPath, Event{Type: "publish_path_classified", Project: filepath.Base(projectPath), Reason: string(cls),
				Detail: "pr-merge push failure classified " + string(cls) + " for " + branch + ": " + err.Error(),
				Fields: map[string]any{"branch": branch, "class": string(cls), "reason": err.Error(), "project_path": projectPath}})
			log.Printf("[Publish] PR merge failed for %s/%s: %v (class=%s)", projectPath, branch, err, cls)
			AppendActivity(projectPath, "publish", "PR merge failed: "+err.Error())
			return false, "pr merge failed: " + mergeErr
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
		cls := ClassifyPushFailure(reason)
		action := ResolvePublishPath(cls, IsHydraOwnedBranch(branch, nil), true)
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_path_classified", Project: filepath.Base(projectPath), Reason: string(cls),
			Detail: "push failure classified " + string(cls) + " -> " + string(action) + " for " + branch + ": " + reason,
			Fields: map[string]any{"branch": branch, "class": string(cls), "action": string(action), "reason": reason, "project_path": projectPath}})
		log.Printf("[Publish] push rejected for %s/%s: %s (class=%s action=%s)", projectPath, branch, reason, cls, action)
		if strings.HasPrefix(reason, "remote at") {
			log.Printf("[Publish] push verification FAILED for %s/%s: %s", projectPath, branch, reason)
		}
		if cls == ClassProtected {
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
// --- FR-011: auto rebase on PR conflict (cap 2 attempts, then human) ---

// MaxPRRebaseAttempts bounds how many times a conflicted PR publish may
// rebase and retry before escalating to a human.
const MaxPRRebaseAttempts = 2

// prConflictError reports whether a publish error is a PR merge conflict
// (as opposed to a network, credential, or verification failure). gh phrasing
// varies ("not mergeable", "has conflicts"), so match on the common stems.
var prConflictError = func(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "conflict") || strings.Contains(s, "not mergeable") ||
		strings.Contains(s, "mergeable state")
}

var (
	// rebaseCheckoutOntoBase rebases the checked-out working branch (HEAD)
	// onto origin/<base>. Overridden in tests.
	rebaseCheckoutOntoBase = defaultRebaseCheckoutOntoBase
	// rebaseBranchOntoBase rebases an arbitrary local branch onto
	// origin/<base> inside a scratch worktree and moves the branch ref.
	// Overridden in tests.
	rebaseBranchOntoBase = defaultRebaseBranchOntoBase
)

// mergeTreeConflicts previews a merge between head and onto with
// `git merge-tree --write-tree --name-only` and returns the conflicting
// file paths (nil = clean merge). The caller decides fail-open vs
// fail-closed: a merge-tree execution error (not a conflict exit) is
// returned so the precheck can never silently block a healthy publish.
func mergeTreeConflicts(projectPath, head, onto string) ([]string, error) {
	out, err := gitOut(projectPath, "merge-tree", "--write-tree", "--name-only", head, onto)
	if err == nil {
		return nil, nil
	}
	if strings.TrimSpace(out) == "" {
		return nil, fmt.Errorf("merge-tree %s %s: %w", head, onto, err)
	}
	// Conflict exit: output is the written tree OID on line 1, then one
	// conflicted path per line (--name-only).
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var files []string
	for _, l := range lines[1:] {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// precheckRebaseConflicts previews whether replaying head onto origin/base
// would conflict. Fail-open: if the preview itself cannot run, log and let
// the rebase proceed (the precheck is an optimization, not a gate). A
// predicted conflict fails closed BEFORE any rebase starts — the observed
// wedge (add/add conflicts in handoff/discovery docs) left a rebase
// in-progress in the daemon's own checkout, blocking all later publishing.
func precheckRebaseConflicts(projectPath, head, base string) error {
	files, err := mergeTreeConflicts(projectPath, head, "origin/"+base)
	if err != nil {
		log.Printf("[Publish] rebase precheck unavailable (%v) — proceeding", err)
		return nil
	}
	if len(files) == 0 {
		return nil
	}
	publishEvents.EmitRepo(projectPath, Event{Type: "publish_rebase_precheck_conflict",
		Detail: fmt.Sprintf("rebase onto origin/%s would conflict in %s — not started, human review needed", base, strings.Join(files, ", "))})
	return fmt.Errorf("rebase onto origin/%s would conflict in %s — rebase not started, human review needed (resolve manually or abandon the branch)", base, strings.Join(files, ", "))
}

// defaultRebaseCheckoutOntoBase rebases the checkout's current branch onto
// origin/<base> so a conflicted PR publish can retry against the advanced
// base. --autostash preserves uncommitted human WIP; hooks are disabled for
// the replayed commits (headless daemon, gates already ran).
func defaultRebaseCheckoutOntoBase(projectPath, base string) error {
	if _, err := gitOut(projectPath, "fetch", "--quiet", "origin", base); err != nil {
		return fmt.Errorf("fetch origin/%s: %w", base, err)
	}
	if err := precheckRebaseConflicts(projectPath, "HEAD", base); err != nil {
		return err
	}
	out, err := gitOut(projectPath, "-c", "core.hooksPath=/dev/null", "rebase", "--autostash", "origin/"+base)
	if err != nil {
		_, _ = gitOut(projectPath, "rebase", "--abort")
		return fmt.Errorf("rebase onto origin/%s: %s", base, strings.TrimSpace(out))
	}
	return nil
}

// defaultRebaseBranchOntoBase rebases a named local branch onto origin/<base>
// without touching any checkout: it replays the branch (detached scratch
// worktree checked out at the branch) onto origin/<base> and moves the branch
// ref to the replayed tip. A real-code conflict aborts the rebase and fails
// closed. The branch's own worktree (if any) may
// end up stale until the caller tears it down or re-checks out — acceptable
// because FR-011 only rebase-retries daemon-owned branches.
func defaultRebaseBranchOntoBase(projectPath, branch, base string) error {
	if _, err := gitOut(projectPath, "fetch", "--quiet", "origin", base); err != nil {
		return fmt.Errorf("fetch origin/%s: %w", base, err)
	}
	scratch, err := os.MkdirTemp("", "hydra-rebase-")
	if err != nil {
		return fmt.Errorf("rebase scratch dir: %w", err)
	}
	defer func() {
		_, _ = gitOut(projectPath, "worktree", "remove", scratch, "--force")
		_, _ = gitOut(projectPath, "worktree", "prune")
	}()
	if _, err := gitOut(projectPath, "worktree", "add", "--detach", scratch, branch); err != nil {
		return fmt.Errorf("rebase scratch worktree: %w", err)
	}
	if err := precheckRebaseConflicts(projectPath, branch, base); err != nil {
		return err
	}
	out, err := gitOut(scratch, "-c", "core.hooksPath=/dev/null", "rebase", "origin/"+base)
	if err != nil {
		_, _ = gitOut(scratch, "rebase", "--abort")
		return fmt.Errorf("rebase %s onto origin/%s: %s", branch, base, strings.TrimSpace(out))
	}
	tip, err := gitOut(scratch, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("rebase %s: read replayed tip: %w", branch, err)
	}
	if _, err := gitOut(projectPath, "update-ref", "refs/heads/"+branch, strings.TrimSpace(tip)); err != nil {
		return fmt.Errorf("move %s to replayed tip: %w", branch, err)
	}
	return nil
}

// publishWithRebaseRetry wraps one PR publish step with FR-011 conflict
// recovery: on a PR-conflict failure it rebases (up to MaxPRRebaseAttempts
// times) and retries; a rebase failure or a still-conflicted PR after the
// cap fails closed with an explicit human-review-needed error. Callers
// already treat publish errors as not-landed, so the escalation surfaces in
// the existing quarantine/Activity channels.
func publishWithRebaseRetry(publish func() (string, error), rebase func() error, projectPath, base string) (string, error) {
	url, err := publish()
	// Sensor symmetry: the rebase-retry sensor's correlation ID pairs each
	// publish_rebase_attempted fire with the publish_succeeded resolve.
	corr := SensorCorrelation("publish-retry", projectPath)
	for attempt := 1; err != nil && prConflictError(err) && attempt <= MaxPRRebaseAttempts; attempt++ {
		log.Printf("[Publish] PR conflict detected — rebase attempt %d/%d onto origin/%s", attempt, MaxPRRebaseAttempts, base)
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_rebase_attempted",
			Detail: fmt.Sprintf("PR conflict: rebase attempt %d/%d onto origin/%s", attempt, MaxPRRebaseAttempts, base),
			Fields: map[string]any{"attempt": attempt, "correlation_id": corr}})
		if rerr := rebase(); rerr != nil {
			return url, fmt.Errorf("pr conflict: rebase attempt %d failed — human review needed: %w", attempt, rerr)
		}
		url, err = publish()
	}
	if err != nil && prConflictError(err) {
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_rebase_exhausted",
			Detail: fmt.Sprintf("PR still conflicted after %d rebase attempts — human review needed", MaxPRRebaseAttempts),
			Fields: map[string]any{"correlation_id": corr}})
		return url, fmt.Errorf("pr still conflicted after %d rebase attempts — human review needed: %w", MaxPRRebaseAttempts, err)
	}
	if err == nil && url != "" {
		// Sensor symmetry (resolve): a completed publish resolves the
		// retry sensor's fires. correlation_id pairs with the attempted fires.
		publishEvents.EmitRepo(projectPath, Event{Type: "publish_succeeded",
			Detail: fmt.Sprintf("publish completed for %s after rebase-retry path", projectPath),
			Fields: map[string]any{"url": url, "correlation_id": corr}})
	}
	return url, err
}

func SetPublishEventSink(s *EventSink) { publishEvents = s }

// PR-merge exec-layer hooks. Overridden in tests to avoid real git/gh calls.
var (
	prMergeGitPush    = defaultPRMergeGitPush
	autoPRGhMerge     = defaultAutoPRGhMerge
	prMergeBranchPush = defaultPRMergeBranchPush
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

// isPRAlreadyExists reports the benign duplicate-PR race: a PR for this
// dogfood head into the same base already exists from an earlier close or
// integration-retest attempt, so `gh pr create` fails with "a pull request
// for branch \"<head>\" into branch \"<base>\" already exists: <URL>"
// (fleet-wide wedge 2026-09-29: qj74/otae/0po4/242b/ycmu/… hard-parked
// integration-exhausted on an open PR that only needed a merge). The
// existing PR is the landing vehicle — treat creation as satisfied; the
// caller's merge step operates on the existing PR.
func isPRAlreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already exists") && strings.Contains(err.Error(), "a pull request for branch")
}

// PRWaitingError reports that the integration PR was created/left open but
// NOT merged: the repo is not on the tier-1 auto-merge allowlist
// (output.pr_auto_merge_repos / deploy.repo_path), so the close is treated
// as waiting-on-human-merge, not an integration failure (operator publish
// policy 2026-09-29). Callers detect it with errors.As.
type PRWaitingError struct{ URL string }

func (e *PRWaitingError) Error() string {
	return "PR left open — waiting on human merge: " + e.URL
}

// prAutoMergeGate decides whether projectPath may admin-merge its PRs.
// nil gate = legacy behavior (always merge). Set via SetPRAutoMergeGate.
var prAutoMergeGate func(projectPath string) bool

// SetPRAutoMergeGate installs the tier-1 auto-merge allowlist gate. The
// daemon wires this from output.pr_auto_merge_repos + deploy.repo_path at
// startup. nil gate (unset) preserves the legacy always-merge behavior.
func SetPRAutoMergeGate(gate func(projectPath string) bool) {
	prAutoMergeGate = gate
}

// prMergeAllowed reports whether projectPath may admin-merge.
func prMergeAllowed(projectPath string) bool {
	return prAutoMergeGate == nil || prAutoMergeGate(projectPath)
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
	// Owner-identity fail-closed: an empty caller token resolves the bead
	// owner's publish identity (project -> owning user -> owner PAT). An
	// unresolved identity blocks the publish — no daemon-credential fallback
	// for non-daemon-owned projects (OPERATOR RULE 2026-09-29).
	if token == "" {
		id, iderr := resolvePublishIdentityFor(projectPath)
		if iderr != nil {
			return "", fmt.Errorf("pr merge: %w", iderr)
		}
		token = id.Token
	}
	if err := prMergeGitPush(projectPath, "refs/heads/"+featureBranch, token); err != nil {
		return "", fmt.Errorf("pr merge: push %s: %w", featureBranch, err)
	}
	if title == "" {
		title = "Hydra dogfood PR"
	}
	url, err := resolveOrCreatePR(projectPath, base, featureBranch, title, "Authored by a Hydra dogfood run.", token)
	if err != nil {
		// resolveOrCreatePR already resolved a pre-existing PR when the create
		// reported "already exists"; an error here means no resolvable PR —
		// fail closed instead of merging blind (hydra-6zwl, 2026-09-29).
		return "", fmt.Errorf("pr merge: create PR: %w", err)
	}
	if err := mergeOrLeaveOpen(projectPath, base, url, featureBranch, token); err != nil {
		return url, err
	}
	return url, nil
}

// mergeOrLeaveOpen admin-merges the PR only under the conservative tier-1
// policy (operator 2026-09-29): the repo must be on the auto-merge allowlist
// (hydra repo — output.pr_auto_merge_repos / deploy.repo_path) AND the PR
// base must be "development" (until FR-030 lands). Otherwise the PR is left
// open and a PRWaitingError is returned so the caller treats the close as
// waiting-on-human-merge.
func mergeOrLeaveOpen(projectPath, base, url, featureBranch, token string) error {
	// Base restriction applies only when a gate is installed; nil gate =
	// legacy always-merge (tests, unconfigured daemons).
	if !prMergeAllowed(projectPath) || (prAutoMergeGate != nil && base != "development") {
		log.Printf("[Publish] %s → %s: tier policy — PR left open (waiting on human merge): %s", projectPath, base, url)
		return &PRWaitingError{URL: url}
	}
	if err := autoPRGhMerge(projectPath, featureBranch, token); err != nil {
		return fmt.Errorf("pr merge: merge %s: %w", url, err)
	}
	return nil
}

func defaultPRMergeGitPush(repoPath, ref, token string) error {
	return defaultPRMergeBranchPush(repoPath, "HEAD", ref, token)
}

// defaultPRMergeBranchPush pushes an explicit local ref (a branch, not
// necessarily the checkout's HEAD) to the PR head ref. FR-010's PR-first
// close pushes the singleton's worktree branch straight to origin instead of
// merging it locally first.
func defaultPRMergeBranchPush(repoPath, localRef, ref, token string) error {
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
	if _, err := gitOut(repoPath, "push", "--force", "--no-verify", target, localRef+":"+ref); err != nil {
		return fmt.Errorf("push HEAD:%s: %s", ref, redactToken(err.Error(), token))
	}
	return nil
}

// defaultAutoPRGhMerge merges the PR for head with a true merge commit,
// bypassing review requirements with --admin (branch protection on the base
// is permanent, so review-gated merges would stall every close). Fail-closed:
// a zero exit alone is not proof the merge landed — only MERGED state from
// `gh pr view` is.
// PublishBranchViaPR lands an existing local branch onto a base branch via
// PR — the FR-010 PR-first close contract. It pushes localBranch to the
// feature branch, opens a PR into base, merges with `gh pr merge --merge
// --admin`, and verifies MERGED state before reporting success. Identical
// contract to PublishViaPR except the pushed ref is a named branch (the
// singleton worktree branch) rather than the checkout's HEAD, so no local
// merge into the working branch is needed. Fail-closed: any step error is
// returned with the URL of a created-but-unmerged PR.
//
// FR-011: on a PR merge conflict the local branch is rebased onto
// origin/<base> and the publish retried, up to MaxPRRebaseAttempts; after
// the cap (or a failed rebase) the error explicitly requests human review.
func PublishBranchViaPR(projectPath, base, localBranch, featureBranch, title, token string) (string, error) {
	return publishWithRebaseRetry(
		func() (string, error) {
			return publishBranchViaPROnce(projectPath, base, localBranch, featureBranch, title, token)
		},
		func() error { return rebaseBranchOntoBase(projectPath, localBranch, base) },
		projectPath, base)
}

// publishBranchViaPROnce is one unguarded PublishBranchViaPR attempt.
func publishBranchViaPROnce(projectPath, base, localBranch, featureBranch, title, token string) (string, error) {
	// resolveOrCreatePR reuses an existing PR when create reports it already
	// exists; a surfaced error means no resolvable PR — fail closed.
	return publishBranchViaPRCreate(projectPath, base, localBranch, featureBranch, title, token)
}

func publishBranchViaPRCreate(projectPath, base, localBranch, featureBranch, title, token string) (string, error) {
	if projectPath == "" || base == "" || localBranch == "" || featureBranch == "" {
		return "", fmt.Errorf("pr merge: empty project, base, local branch, or feature branch")
	}
	if token == "" {
		id, iderr := resolvePublishIdentityFor(projectPath)
		if iderr != nil {
			return "", fmt.Errorf("pr merge: %w", iderr)
		}
		token = id.Token
	}
	if err := prMergeBranchPush(projectPath, localBranch, "refs/heads/"+featureBranch, token); err != nil {
		return "", fmt.Errorf("pr merge: push %s: %w", featureBranch, err)
	}
	if title == "" {
		title = "Hydra dogfood PR"
	}
	url, err := resolveOrCreatePR(projectPath, base, featureBranch, title, "Authored by a Hydra dogfood run.", token)
	if err != nil {
		// resolveOrCreatePR already resolved a pre-existing PR when the create
		// reported "already exists"; an error here means no resolvable PR —
		// fail closed instead of merging blind (hydra-6zwl, 2026-09-29).
		return "", fmt.Errorf("pr merge: create PR: %w", err)
	}
	if err := mergeOrLeaveOpen(projectPath, base, url, featureBranch, token); err != nil {
		return url, err
	}
	return url, nil
}

func defaultAutoPRGhMerge(repoPath, head, token string) error {
	env := os.Environ()
	if token != "" {
		env = append(env, "GH_TOKEN="+token)
	}
	merge := exec.Command("gh", "pr", "merge", head, "--merge", "--admin", "--delete-branch")
	merge.Dir = repoPath
	merge.Env = env
	if out, err := merge.CombinedOutput(); err != nil {
		// Benign race: the PR reached MERGED between create/resolve and this
		// merge call (duplicate integration). Verify state before failing.
		state, viewErr := ghPRState(repoPath, head, env)
		if viewErr == nil && state == "MERGED" {
			return nil
		}
		return fmt.Errorf("gh pr merge: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	// gh pr merge exit 0 is authoritative for the merge itself, but the
	// immediately-following state read can hit GitHub API eventual
	// consistency (observed 2026-09-29: PR #401 merged at 18:27:59Z, state
	// read at 18:28:02Z returned OPEN) — a single stale OPEN was counted as
	// an integration failure and drove the bead to integration-exhausted
	// hard-park. Retry the read before failing.
	state := ""
	var err error
	for attempt := 0; attempt < mergeStateVerifyAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(mergeStateVerifyBackoff * time.Duration(attempt))
		}
		state, err = ghPRState(repoPath, head, env)
		if err != nil {
			return err
		}
		if state == "MERGED" {
			return nil
		}
	}
	return fmt.Errorf("gh pr view: state=%q, want MERGED (after %d attempts)", state, mergeStateVerifyAttempts)
}

// CleanupMergedIntegrationBranches deletes remote integration branches
// (dogfood/ or hydra/ — IsHydraOwnedBranch) whose PRs reached MERGED
// (hydra-9dp5). The auto-merge path already deletes its head via
// --delete-branch; this sweep catches human-merged PRs on tier-2 product
// repos. Fail-open and quiet: any gh/push fault is logged, never returned.
func CleanupMergedIntegrationBranches(projectPath, token string) {
	env := os.Environ()
	if token != "" {
		env = append(env, "GH_TOKEN="+token)
	}
	list := exec.Command("gh", "pr", "list", "--state", "merged", "--json", "headRefName", "--limit", "200")
	list.Dir = projectPath
	list.Env = env
	out, err := list.CombinedOutput()
	if err != nil {
		log.Printf("[Publish] merged-branch cleanup %s: gh pr list failed (not blocking): %v", projectPath, err)
		return
	}
	var prs []struct {
		HeadRefName string `json:"headRefName"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		log.Printf("[Publish] merged-branch cleanup %s: parse failed (not blocking): %v", projectPath, err)
		return
	}
	originURL, err := gitOut(projectPath, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(originURL) == "" {
		return
	}
	authURL := authenticatedURL(originURL, token)
	if authURL == "" {
		return
	}
	for _, pr := range prs {
		head := pr.HeadRefName
		if !IsHydraOwnedBranch(head, nil) {
			continue
		}
		cmd := exec.Command("git", "-C", projectPath, "push", "--no-verify", authURL, "--delete", head)
		if out, err := cmd.CombinedOutput(); err != nil {
			// Ref already gone or never pushed — benign; keep sweeping.
			log.Printf("[Publish] merged-branch cleanup %s: delete %s failed (not blocking): %s", projectPath, head, strings.TrimSpace(string(out)))
			continue
		}
		log.Printf("[Publish] merged-branch cleanup %s: deleted merged integration branch %s", projectPath, head)
	}
}

// ghPRState reads a PR's state via gh pr view. env carries GH_TOKEN when set.
// Overridden in tests.
var ghPRState = defaultGhPRState

// Post-merge state-verify retry budget: GitHub API eventual consistency can
// serve a stale OPEN for a few seconds right after `gh pr merge` succeeds.
const (
	mergeStateVerifyAttempts = 4
	mergeStateVerifyBackoff  = 2 * time.Second
)

func defaultGhPRState(repoPath, head string, env []string) (string, error) {
	view := exec.Command("gh", "pr", "view", head, "--json", "state", "--jq", ".state")
	view.Dir = repoPath
	view.Env = env
	out, err := view.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr view: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
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
// Owner-identity: every publish-path git/GitHub op runs under the bead
// owner's PAT + git identity, fail-closed with no fallbacks (OPERATOR RULE
// 2026-09-29). maybeOpenAutoPR is fail-open itself: an unresolved identity
// logs and returns false (the caller then fails the integration closed).
func maybeOpenAutoPR(beadsClient BeadsClient, cfg *Config, projectPath, repoName, branch string, bead *types.Bead) bool {
	if cfg == nil || !cfg.AutoPREnabled() || beadsClient == nil || bead == nil || branch == "" {
		return false
	}
	head := IntegrationBranchPrefix(projectPath) + string(bead.ID)
	for _, l := range bead.Labels {
		if l == "pr-open" {
			log.Printf("[AutoPR] skipping %s: already has pr-open label", bead.ID)
			return false
		}
	}
	id, iderr := resolvePublishIdentityFor(projectPath)
	if iderr != nil {
		log.Printf("[AutoPR] %s: %v (fail closed)", bead.ID, iderr)
		return false
	}
	token := id.Token
	if out, err := autoPRLsRemote(projectPath, "origin", "refs/heads/"+head); err == nil && strings.TrimSpace(out) != "" {
		open, err := autoPRGhListOpen(projectPath, head, token)
		if err != nil {
			log.Printf("[AutoPR] %s: listing PRs for %s: %v (fail open)", bead.ID, head, err)
			return false
		}
		if open {
			// The branch is already carried by an open PR — but the earlier
			// integration may never have merged it. Land it now; only a
			// verified MERGED state counts as preserved. Non-allowlisted
			// (product) repos leave the PR open — waiting-on-human counts.
			if !prMergeAllowed(projectPath) || (prAutoMergeGate != nil && branch != "development") {
				log.Printf("[AutoPR] %s: open PR for %s left for human merge (tier policy: %s → %s not auto-mergeable)", bead.ID, head, branch, head)
				return true
			}
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

// resolveExistingPR looks up the open-or-merged PR already tracking head.
// Used when gh pr create fails with "a pull request for branch ... already
// exists": the wedge that hard-parked seven srv-telematics beads in a row
// (2026-09-29) — the pre-existing PR was never resolved or merged, so every
// retry failed identically until the consecutive-failure cap fired.
// ghPRView looks up an existing PR for head, returning "<url> <state>".
// Overridden in tests.
var ghPRView = defaultGhPRView

func defaultGhPRView(repoPath, head, token string) (string, error) {
	cmd := exec.Command("gh", "pr", "view", head, "--json", "url,state", "--jq", ".url + \" \" + .state")
	cmd.Dir = repoPath
	if token != "" {
		cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr view %s: %w\n%s", head, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// resolveOrCreatePR returns autoPRGhCreate's URL, falling back to resolving
// the pre-existing PR when create reports "already exists".
func resolveOrCreatePR(repoPath, base, head, title, body, token string) (string, error) {
	// Pre-check before create: a PR for this head may already exist (created
	// by an earlier close attempt that failed at a later step). Skip create
	// and reuse the existing PR; a list error fails open to the create path.
	if open, lerr := autoPRGhListOpen(repoPath, head, token); lerr == nil && open {
		if url, rerr := resolveExistingPR(repoPath, head, token); rerr == nil {
			log.Printf("[Publish] open PR already tracks %s — skipping create, reusing: %s", head, url)
			return url, nil
		}
	}
	url, err := autoPRGhCreate(repoPath, base, head, title, body, token)
	if err == nil {
		return url, nil
	}
	if isPRAlreadyExists(err) {
		// gh embeds the existing PR's URL in the "already exists" message —
		// that URL is authoritative and needs no extra gh call (hydra-6zwl,
		// 2026-09-29: PR #389 wedged the publish tick for 22m+ on exactly
		// this error).
		if u := prURLFromError(err); u != "" {
			log.Printf("[Publish] PR for %s already exists — reusing PR from create error: %s", head, u)
			return u, nil
		}
		if url, rerr := resolveExistingPR(repoPath, head, token); rerr == nil {
			log.Printf("[Publish] PR for %s already exists — resolving existing PR instead of failing: %s", head, url)
			return url, nil
		}
	}
	return "", err
}

// prURLFromError extracts the first http(s) URL from a gh error message
// (e.g. "a pull request for branch \"x\" into branch \"y\" already exists:\nhttps://github.com/o/r/pull/42").
func prURLFromError(err error) string {
	if err == nil {
		return ""
	}
	for _, tok := range strings.Fields(err.Error()) {
		if strings.HasPrefix(tok, "http://") || strings.HasPrefix(tok, "https://") {
			return strings.TrimRight(tok, ":\",\"")
		}
	}
	return ""
}

func resolveExistingPR(repoPath, head, token string) (string, error) {
	out, err := ghPRView(repoPath, head, token)
	if err != nil {
		return "", err
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return "", fmt.Errorf("gh pr view %s: unexpected output %q", head, out)
	}
	url, state := parts[0], parts[1]
	switch state {
	case "OPEN", "MERGED":
		return url, nil
	default:
		return "", fmt.Errorf("gh pr view %s: existing PR state %q not resolvable", head, state)
	}
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
