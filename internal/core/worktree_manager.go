package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// WorktreeManager provides per-chain git worktree isolation. Each chain (or
// singleton bead) runs in its own git worktree off the project's working
// branch, so concurrent chains cannot clobber each other's files in the
// shared checkout. A bead's changes are committed into the chain's worktree
// branch as it closes; when the chain completes the branch is integrated back
// into the working branch and the worktree removed.
//
// Worktrees live OUTSIDE the target project (a sibling `.hydra-worktrees/`
// dir) so they never appear in the target project's own git status.
type WorktreeManager struct {
	mu sync.Mutex // serialize git worktree ops (shared repo, single process)
}

// NewWorktreeManager creates a worktree manager.
func NewWorktreeManager() *WorktreeManager { return &WorktreeManager{} }

// worktreeRoot returns the directory that holds a project's worktrees. It
// lives OUTSIDE the target project (so worktrees never pollute the project's
// git status) AND under a base the hydra daemon owns — not the project's
// parent dir, which may be owned by another user (e.g. a per-user project
// under /home/<user>) and therefore not writable by the daemon. Base is
// $HYDRA_HOME when set, else the system temp dir. The full project path is
// sanitized into the subdir name to keep worktrees unique across projects.
func worktreeRoot(projectPath string) string {
	base := os.Getenv("HYDRA_HOME")
	if base == "" {
		base = os.TempDir()
	}
	clean := strings.TrimRight(projectPath, string(os.PathSeparator))
	return filepath.Join(base, ".hydra-worktrees", sanitizeKey(clean))
}

func sanitizeKey(key string) string {
	return strings.NewReplacer("/", "-", " ", "-", ":", "-", string(os.PathSeparator), "-").Replace(key)
}

func (m *WorktreeManager) branchName(key string) string { return "hydra/wt-" + sanitizeKey(key) }

func (m *WorktreeManager) worktreePath(projectPath, key string) string {
	return filepath.Join(worktreeRoot(projectPath), sanitizeKey(key))
}

// WorkingBranch returns the project checkout's current branch name.
func (m *WorktreeManager) WorkingBranch(projectPath string) (string, error) {
	out, err := gitOut(projectPath, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out), err
}

// EnsureWorktree creates the chain's worktree off the working branch if it
// does not exist and returns its path. Idempotent — reuses an existing one.
func (m *WorktreeManager) EnsureWorktree(projectPath, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	wtPath := m.worktreePath(projectPath, key)
	if isGitWorktree(wtPath) {
		return wtPath, nil
	}
	if err := os.MkdirAll(worktreeRoot(projectPath), 0o755); err != nil {
		return "", err
	}

	base, err := m.WorkingBranch(projectPath)
	if err != nil || base == "" || base == "HEAD" {
		base = "HEAD"
	}
	branch := m.branchName(key)

	if branchExists(projectPath, branch) {
		// Stale branch from a prior run — attach a worktree to it.
		if _, err := gitOut(projectPath, "worktree", "add", wtPath, branch); err != nil {
			return "", fmt.Errorf("worktree add (existing branch %s): %w", branch, err)
		}
		return wtPath, nil
	}
	if _, err := gitOut(projectPath, "worktree", "add", "-b", branch, wtPath, base); err != nil {
		return "", fmt.Errorf("worktree add %s off %s: %w", branch, base, err)
	}
	return wtPath, nil
}

// Commit stages everything in the worktree and commits it. Returns
// (false, nil) when there is nothing to commit (no work was done).
func (m *WorktreeManager) Commit(worktreePath, message string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := gitOut(worktreePath, "add", "-A"); err != nil {
		return false, err
	}
	// `diff --cached --quiet` exits 0 when nothing is staged.
	if _, err := gitOut(worktreePath, "diff", "--cached", "--quiet"); err == nil {
		return false, nil
	}
	if _, err := gitOut(worktreePath, "commit", "-m", message); err != nil {
		return false, fmt.Errorf("commit in %s: %w", worktreePath, err)
	}
	return true, nil
}

// IntegrateAndRemove merges the chain's branch into the working branch (in the
// main checkout) and removes the worktree + branch. On merge conflict it
// aborts the merge, leaves the worktree in place for inspection, and errors.
func (m *WorktreeManager) IntegrateAndRemove(projectPath, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wtPath := m.worktreePath(projectPath, key)
	branch := m.branchName(key)
	if !isGitWorktree(wtPath) && !branchExists(projectPath, branch) {
		return nil // nothing to integrate
	}

	if _, err := gitOut(projectPath, "merge", "--no-ff", "-m", "hydra: integrate "+branch, branch); err != nil {
		_, _ = gitOut(projectPath, "merge", "--abort")
		return fmt.Errorf("integrate %s failed (worktree left at %s for inspection): %w", branch, wtPath, err)
	}

	if isGitWorktree(wtPath) {
		_, _ = gitOut(projectPath, "worktree", "remove", wtPath, "--force")
	}
	_, _ = gitOut(projectPath, "branch", "-D", branch)
	_, _ = gitOut(projectPath, "worktree", "prune")
	return nil
}

// Remove tears down a chain's worktree + branch without integrating (used to
// discard abandoned/failed chains).
func (m *WorktreeManager) Remove(projectPath, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wtPath := m.worktreePath(projectPath, key)
	if isGitWorktree(wtPath) {
		_, _ = gitOut(projectPath, "worktree", "remove", wtPath, "--force")
	}
	_, _ = gitOut(projectPath, "branch", "-D", m.branchName(key))
	_, _ = gitOut(projectPath, "worktree", "prune")
}

// --- helpers ---

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func isGitWorktree(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func branchExists(projectPath, branch string) bool {
	_, err := gitOut(projectPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}
