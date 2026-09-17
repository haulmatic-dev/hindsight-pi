package core

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// authenticatedURL converts a git origin URL (ssh or https) into an HTTPS URL
// with the token embedded, so a push authenticates as the token's owner
// without persisting credentials in the repo config. Returns "" when the
// origin URL cannot be understood (caller falls back to the plain remote).
//
// Examples:
//
//	git@github.com:org/repo.git        -> https://x-access-token:TOKEN@github.com/org/repo.git
//	https://github.com/org/repo.git    -> https://x-access-token:TOKEN@github.com/org/repo.git
//	https://gitlab.com/g/s/repo.git    -> https://x-access-token:TOKEN@gitlab.com/g/s/repo.git
func authenticatedURL(originURL, token string) string {
	origin := strings.TrimSpace(originURL)
	if origin == "" || token == "" {
		return ""
	}
	var hostPath string
	switch {
	case strings.HasPrefix(origin, "git@"):
		// git@host:path → host/path
		rest := strings.TrimPrefix(origin, "git@")
		rest = strings.Replace(rest, ":", "/", 1)
		hostPath = rest
	case strings.HasPrefix(origin, "https://"):
		rest := strings.TrimPrefix(origin, "https://")
		// Drop any existing userinfo (user:pass@host…).
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		hostPath = rest
	case strings.HasPrefix(origin, "http://"):
		rest := strings.TrimPrefix(origin, "http://")
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		hostPath = rest
	default:
		return ""
	}
	if hostPath == "" {
		return ""
	}
	return fmt.Sprintf("https://x-access-token:%s@%s", token, hostPath)
}

// redactToken removes a token substring from text so it never reaches logs.
func redactToken(text, token string) string {
	if token == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "***")
}

// verifyRemoteHead checks that the remote-tracking ref for branch matches the
// local branch head. It returns (true, "", "") on match and (false, localSHA,
// remoteSHA) on mismatch or error. The comparison uses ls-remote so it does not
// trust the local remote-tracking ref, which can be updated even when the
// remote silently ignores the push.
func verifyRemoteHead(repoPath, remote, branch string) (bool, string, string) {
	localOut, err := gitOut(repoPath, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return false, "", ""
	}
	localSHA := strings.TrimSpace(localOut)
	remoteOut, err := gitOut(repoPath, "ls-remote", remote, "refs/heads/"+branch)
	if err != nil {
		return false, localSHA, ""
	}
	fields := strings.Fields(remoteOut)
	if len(fields) == 0 {
		return false, localSHA, ""
	}
	remoteSHA := fields[0]
	if localSHA == remoteSHA {
		return true, localSHA, remoteSHA
	}
	return false, localSHA, remoteSHA
}

// IsProtectedBranchRejection reports whether a push-failure reason indicates
// the remote rejected the push via branch protection (GH006, pre-receive hook
// decline, or a non-FF "remote rejected"). Callers use it to route the
// failure to the auto-PR fallback; the full reason text is kept verbatim so
// the exact remote error always reaches logs and events.
func IsProtectedBranchRejection(reason string) bool {
	lower := strings.ToLower(reason)
	return strings.Contains(lower, "protected branch") ||
		strings.Contains(lower, "gh006") ||
		strings.Contains(lower, "pre-receive hook declined") ||
		strings.Contains(lower, "remote rejected") ||
		strings.Contains(lower, "changes must be made through a pull request") ||
		// Non-fast-forward: the remote diverged (another pusher, or a
		// protected branch's own merge queue). Hydra-7k1 2026-09-16: local
		// development was ahead with unpushed integrates AND behind origin
		// (PR #253 merged out of band), the push died "(non-fast-forward)",
		// this classifier missed it, no auto-PR opened, and the bead
		// reopened into an empty-diff loop. The doc comment above already
		// promised this coverage; the implementation did not deliver it.
		// Rule violations: GitHub rulesets phrase a declined push as
		// "push declined due to repository rule violations" (seen on
		// haulmatic-dev/hydra development 2026-09-17 after divergence was
		// resolved — the protection is permanent, so this is the steady-state
		// rejection and must route to the auto-PR fallback).
		strings.Contains(lower, "rule violations") ||
		strings.Contains(lower, "non-fast-forward") ||
		strings.Contains(lower, "fetch first")
}

// PushBranchWithNonFFRecovery is PushBranch with one non-fast-forward
// recovery round: when the remote branch has diverged from local (integrates
// that landed out of band, another daemon pushing the same working branch),
// fetch the remote tip and merge it (--no-edit) so local again contains the
// remote tip, then retry the push once. A merge is used rather than a rebase
// because the working branch carries --no-ff "hydra: integrate …" merge
// commits that a rebase would flatten. A conflicting merge is aborted and the
// original push-failure reason is returned verbatim so callers can still
// route it (auto-PR, fail-closed reopen). Any non-non-FF failure, or a
// missing remote ref, passes straight through unchanged.
func PushBranchWithNonFFRecovery(repoPath, remote, branch string) (bool, string) {
	pushed, reason := PushBranch(repoPath, remote, branch)
	if pushed || !IsProtectedBranchRejection(reason) || !strings.Contains(strings.ToLower(reason), "non-fast-forward") {
		return pushed, reason
	}
	if _, err := gitOut(repoPath, "fetch", remote, branch); err != nil {
		log.Printf("[Push] non-FF recovery fetch %s/%s failed (not blocking): %v", remote, branch, err)
		return pushed, reason
	}
	if _, err := gitOut(repoPath, "merge", "--no-edit", "FETCH_HEAD"); err != nil {
		// Conflicting or otherwise unmergeable remote tip: restore the
		// pre-merge state and report the original push failure.
		_, _ = gitOut(repoPath, "merge", "--abort")
		log.Printf("[Push] non-FF recovery merge of %s/%s failed (not blocking): %v", remote, branch, err)
		return pushed, reason
	}
	return PushBranch(repoPath, remote, branch)
}

// PushBranchWithToken pushes branch from repoPath to the repo's origin,
// authenticating with the given personal access token (the project owner's).
// It resolves origin, injects the token into a one-shot HTTPS URL (never
// persisted to config), and pushes. Fail-open and SILENT like PushBranch:
// returns (false, reason) on any failure, never errors/panics, and the token
// is redacted from every returned reason. The reason carries the exact
// (redacted) remote stderr on rejection — never a collapsed summary — so
// branch-protection rejections (GH006) surface verbatim. Falls back to a
// plain `origin` push when the token is empty or the origin URL cannot be
// parsed.
func PushBranchWithToken(repoPath, branch, token string) (bool, string) {
	if branch == "" {
		return false, "not configured"
	}
	originURL, err := gitOut(repoPath, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(originURL) == "" {
		return false, "no remote"
	}
	authURL := authenticatedURL(originURL, token)
	if authURL == "" {
		// No usable token/URL — fall back to a plain remote push.
		return PushBranch(repoPath, "origin", branch)
	}

	// --no-verify: client-side hooks (e.g. husky pre-push running npm test:cov)
	// are interactive-dev affordances; the daemon is a headless release pipeline
	// that re-runs its own gates, and gutted node_modules in a checkout makes
	// those hooks fail ("husky: command not found") and silently block pushes.
	cmd := exec.Command("git", "-C", repoPath, "push", "--no-verify", authURL, branch)
	out, cErr := cmd.CombinedOutput()
	if cErr == nil {
		if ok, local, remote := verifyRemoteHead(repoPath, "origin", branch); !ok {
			return false, fmt.Sprintf("remote at %s, local %s", remote, local)
		}
		return true, ""
	}
	msg := redactToken(string(out), token)
	reason := strings.TrimSpace(msg)
	if reason == "" {
		reason = "push failed"
	}
	return false, reason
}

// PushBranch attempts to push branch from repoPath to the named remote.
// It returns (true, "") on success and (false, reason) on any failure.
// It never returns an error value or panics. The reason carries the exact
// remote stderr on rejection (e.g. GH006 branch-protection text) so callers
// can log it verbatim; use IsProtectedBranchRejection to classify it.
//
// The push uses --no-verify: client-side hooks (husky pre-push, etc.) are
// interactive-dev affordances that break in headless daemon checkouts (gutted
// node_modules, missing env) and would silently block the push. The daemon
// re-runs its own verification gates after push, so bypassing them is safe.
// Server-side hooks (pre-receive) still run on the remote and are unaffected.
func PushBranch(repoPath, remote, branch string) (bool, string) {
	if remote == "" {
		return false, "not configured"
	}

	cmd := exec.Command("git", "-C", repoPath, "push", "--no-verify", remote, branch)
	out, err := cmd.CombinedOutput()
	if err == nil {
		if ok, local, rem := verifyRemoteHead(repoPath, remote, branch); !ok {
			return false, fmt.Sprintf("remote at %s, local %s", rem, local)
		}
		return true, ""
	}

	reason := strings.TrimSpace(string(out))
	if reason == "" {
		reason = "push failed"
	}
	return false, reason
}
