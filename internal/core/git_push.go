package core

import (
	"os/exec"
	"strings"
)

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
