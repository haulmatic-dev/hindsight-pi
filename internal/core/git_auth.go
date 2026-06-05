package core

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GitAuth handles per-user git operations with their credentials.
type GitAuth struct{}

// NewGitAuth creates a new git auth manager.
func NewGitAuth() *GitAuth {
	return &GitAuth{}
}

// CommitResult holds the outcome of a git commit/push operation.
type CommitResult struct {
	Success bool
	Hash    string
	Branch  string
	Error   error
}

// CommitAndPush stages changes, commits as the user, and pushes with their PAT.
func (g *GitAuth) CommitAndPush(ctx *UserContext, projectPath, branch, message string) CommitResult {
	// Configure git identity for this repo (does not affect global config)
	if err := g.gitConfig(projectPath, "user.name", ctx.Config.User.Name); err != nil {
		return CommitResult{Error: fmt.Errorf("set user.name: %w", err)}
	}
	if err := g.gitConfig(projectPath, "user.email", ctx.Config.User.Email); err != nil {
		return CommitResult{Error: fmt.Errorf("set user.email: %w", err)}
	}

	// Stage all changes
	if err := g.gitRun(projectPath, "add", "-A"); err != nil {
		return CommitResult{Error: fmt.Errorf("git add: %w", err)}
	}

	// Check if there are changes to commit
	hasChanges, err := g.hasChanges(projectPath)
	if err != nil {
		return CommitResult{Error: fmt.Errorf("check changes: %w", err)}
	}
	if !hasChanges {
		return CommitResult{Success: true, Branch: branch}
	}

	// Commit
	if err := g.gitRun(projectPath, "commit", "-m", message); err != nil {
		return CommitResult{Error: fmt.Errorf("git commit: %w", err)}
	}

	// Get commit hash
	hash, _ := g.getCommitHash(projectPath)

	// Set remote with PAT for push
	remoteURL := ctx.GitRemoteURL(g.getOriginURL(projectPath))
	if remoteURL == "" {
		return CommitResult{Error: fmt.Errorf("no remote origin configured")}
	}

	oldRemote := g.getOriginURL(projectPath)
	if err := g.gitRun(projectPath, "remote", "set-url", "origin", remoteURL); err != nil {
		return CommitResult{Error: fmt.Errorf("set remote: %w", err)}
	}

	// Push
	pushErr := g.gitRun(projectPath, "push", "origin", branch)

	// Restore public remote URL (remove PAT from .git/config)
	if oldRemote != "" {
		_ = g.gitRun(projectPath, "remote", "set-url", "origin", oldRemote)
	}

	if pushErr != nil {
		return CommitResult{Error: fmt.Errorf("git push: %w", pushErr)}
	}

	return CommitResult{Success: true, Hash: hash, Branch: branch}
}

// gitConfig sets a git config value in the project repo (not global).
func (g *GitAuth) gitConfig(projectPath, key, value string) error {
	return g.gitRun(projectPath, "config", key, value)
}

// gitRun executes a git command in the project directory.
func (g *GitAuth) gitRun(projectPath string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = projectPath
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	return nil
}

// hasChanges returns true if the working tree has uncommitted changes.
func (g *GitAuth) hasChanges(projectPath string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// getCommitHash returns the current HEAD hash.
func (g *GitAuth) getCommitHash(projectPath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// getOriginURL returns the current origin remote URL.
func (g *GitAuth) getOriginURL(projectPath string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
