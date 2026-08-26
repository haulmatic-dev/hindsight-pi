package core

import (
	"fmt"
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

// PushBranchWithToken pushes branch from repoPath to the repo's origin,
// authenticating with the given personal access token (the project owner's).
// It resolves origin, injects the token into a one-shot HTTPS URL (never
// persisted to config), and pushes. Fail-open and SILENT like PushBranch:
// returns (false, reason) on any failure, never errors/panics, and the token
// is redacted from every returned reason. Falls back to a plain `origin` push
// when the token is empty or the origin URL cannot be parsed.
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

	cmd := exec.Command("git", "-C", repoPath, "push", authURL, branch)
	out, cErr := cmd.CombinedOutput()
	if cErr == nil {
		if ok, local, remote := verifyRemoteHead(repoPath, "origin", branch); !ok {
			return false, fmt.Sprintf("remote at %s, local %s", remote, local)
		}
		return true, ""
	}
	msg := redactToken(string(out), token)
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "pre-receive hook declined") ||
		strings.Contains(lower, "protected branch") ||
		strings.Contains(lower, "remote rejected") {
		return false, "protected branch"
	}
	if strings.Contains(lower, "authentication") || strings.Contains(lower, "403") ||
		strings.Contains(lower, "could not read username") {
		return false, "auth"
	}
	reason := strings.TrimSpace(msg)
	if reason == "" {
		reason = "push failed"
	}
	return false, reason
}

// PushBranch attempts to push branch from repoPath to the named remote.
// It returns (true, "") on success and (false, reason) on any failure.
// It never returns an error value or panics.
func PushBranch(repoPath, remote, branch string) (bool, string) {
	if remote == "" {
		return false, "not configured"
	}

	cmd := exec.Command("git", "-C", repoPath, "push", remote, branch)
	out, err := cmd.CombinedOutput()
	if err == nil {
		if ok, local, rem := verifyRemoteHead(repoPath, remote, branch); !ok {
			return false, fmt.Sprintf("remote at %s, local %s", rem, local)
		}
		return true, ""
	}

	msg := string(out)
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "pre-receive hook declined") ||
		strings.Contains(lower, "protected branch") ||
		strings.Contains(lower, "remote rejected") {
		return false, "protected branch"
	}

	reason := strings.TrimSpace(msg)
	if reason == "" {
		reason = "push failed"
	}
	return false, reason
}
