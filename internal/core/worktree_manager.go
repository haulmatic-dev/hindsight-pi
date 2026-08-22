package core

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

// ExistingWorktree returns the worktree path for a chain key WITHOUT creating
// it, and whether it currently exists as a git worktree. Used by the
// verified-close diff/scope gates so they inspect the tree the worker actually
// wrote to (the worktree), not the shared checkout (which is clean until the
// chain integrates).
func (m *WorktreeManager) ExistingWorktree(projectPath, key string) (string, bool) {
	wtPath := m.worktreePath(projectPath, key)
	return wtPath, isGitWorktree(wtPath)
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

	// .beads is daemon machinery state (status flips, DB mirror) — never
	// product content. Union-merging id-keyed JSONL produced duplicate rows
	// and broke br entirely (srv soe53, 2026-08-20). Exclude it from commits.
	if _, err := gitOut(worktreePath, "add", "-A", "--", ".", ":(exclude).beads"); err != nil {
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

	// Dirty main checkout blocks merges outright ("Your local changes would
	// be overwritten") — janitorial auto-commit first (2026-08-19 incident).
	autoCommitDirtyMain(projectPath)

	if _, err := gitOut(projectPath, "merge", "--no-ff", "-m", "hydra: integrate "+branch, branch); err != nil {
		// .beads/ is a br-owned mirror: br state on main is the source of
		// truth and worktree-side writes are redundant, so a conflict
		// confined to .beads/ resolves --ours. It quarantined 65wa0's
		// gate-proven fix on a mirror row (2026-08-21). Real-code
		// conflicts still abort + quarantine.
		if beadsOnlyConflict(projectPath) {
			if _, oerr := gitOut(projectPath, "checkout", "--ours", "--", ".beads"); oerr == nil {
				_, _ = gitOut(projectPath, "add", ".beads")
				if _, cerr := gitOut(projectPath, "commit", "--no-edit"); cerr == nil {
					log.Printf("[Worktree] auto-resolved .beads-only merge conflict (--ours) integrating %s", branch)
					goto merged
				}
			}
		}
		_, _ = gitOut(projectPath, "merge", "--abort")
		return fmt.Errorf("integrate %s failed (worktree left at %s for inspection): %w", branch, wtPath, err)
	}

merged:

	if isGitWorktree(wtPath) {
		_, _ = gitOut(projectPath, "worktree", "remove", wtPath, "--force")
	}
	_, _ = gitOut(projectPath, "branch", "-D", branch)
	_, _ = gitOut(projectPath, "worktree", "prune")
	return nil
}

// beadsOnlyConflict reports whether every unmerged path in a failed merge
// lives under .beads/ (the br-owned mirror — safe to resolve --ours).
func beadsOnlyConflict(projectPath string) bool {
	out, err := gitOut(projectPath, "diff", "--name-only", "--diff-filter=U")
	if err != nil || strings.TrimSpace(out) == "" {
		return false
	}
	for _, p := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(p), ".beads/") && strings.TrimSpace(p) != ".beads" {
			return false
		}
	}
	return true
}

// QuarantineAndRemove preserves a failed chain's work on a quarantine branch
// and removes the worktree WITHOUT merging into the working branch.
//
// Failed chains used to integrate like successful ones — the chain-completion
// predicate counted closed-as-failed beads as completed — which left a
// deliberately red test committed on the testbed's working branch (2026-07-28).
// A failed PRD must not contaminate the branch developers pull; its partial
// work is kept on hydra/quarantine/<key> for inspection instead of deleted,
// because the fix bead that retries the work may want the diff.
func (m *WorktreeManager) QuarantineAndRemove(projectPath, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	wtPath := m.worktreePath(projectPath, key)
	branch := m.branchName(key)
	if !isGitWorktree(wtPath) && !branchExists(projectPath, branch) {
		return "", nil // nothing to quarantine
	}

	qBranch := "hydra/quarantine/" + key
	// -f: a re-run of the same chain overwrites its previous quarantine — the
	// newest failed attempt is the one worth inspecting.
	if _, err := gitOut(projectPath, "branch", "-f", qBranch, branch); err != nil {
		return "", fmt.Errorf("quarantine %s failed (worktree left at %s): %w", branch, wtPath, err)
	}

	if isGitWorktree(wtPath) {
		_, _ = gitOut(projectPath, "worktree", "remove", wtPath, "--force")
	}
	_, _ = gitOut(projectPath, "branch", "-D", branch)
	_, _ = gitOut(projectPath, "worktree", "prune")
	return qBranch, nil
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

// CleanupStaleWorktrees removes per-chain worktrees that are older than
// maxAge and not owned by an active worker. Live runs accumulate one worktree
// per bead/chain and nothing removed them (hundreds observed after one run) —
// integration removes a worktree only when its chain completes cleanly, so
// failed/abandoned chains leak. Returns the number of worktrees removed.
//
// Staleness is judged by the worktree's .git file mtime (set at creation).
// The source repository (needed for `git worktree prune`) is recovered from
// the .git file's "gitdir: <src>/.git/worktrees/<name>" pointer.
func CleanupStaleWorktrees(maxAge time.Duration, activeKeys map[string]bool) int {
	base := os.Getenv("HYDRA_HOME")
	if base == "" {
		base = os.TempDir()
	}
	root := filepath.Join(base, ".hydra-worktrees")
	projects, err := os.ReadDir(root)
	if err != nil {
		return 0
	}

	removed := 0
	pruneRepos := map[string]bool{}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		projDir := filepath.Join(root, p.Name())
		entries, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || activeKeys[e.Name()] {
				continue
			}
			wt := filepath.Join(projDir, e.Name())
			gitFile := filepath.Join(wt, ".git")
			info, err := os.Stat(gitFile)
			if err != nil {
				// Not a worktree (junk/partial dir): remove if the dir itself is old.
				if di, derr := os.Stat(wt); derr == nil && time.Since(di.ModTime()) > maxAge {
					_ = os.RemoveAll(wt)
					removed++
				}
				continue
			}
			if time.Since(info.ModTime()) <= maxAge {
				continue
			}
			if src := worktreeSourceRepo(gitFile); src != "" {
				pruneRepos[src] = true
			}
			if err := os.RemoveAll(wt); err != nil {
				log.Printf("[WorktreeGC] remove %s: %v", wt, err)
				continue
			}
			removed++
		}
	}
	for repo := range pruneRepos {
		_, _ = gitOut(repo, "worktree", "prune")
	}
	if removed > 0 {
		log.Printf("[WorktreeGC] removed %d stale worktree(s) under %s", removed, root)
	}
	return removed
}

// worktreeSourceRepo extracts the source repository path from a worktree's
// .git pointer file ("gitdir: <src>/.git/worktrees/<name>").
func worktreeSourceRepo(gitFile string) string {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
	marker := string(os.PathSeparator) + ".git" + string(os.PathSeparator) + "worktrees" + string(os.PathSeparator)
	if i := strings.Index(s, marker); i >= 0 {
		return s[:i]
	}
	return ""
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

// autoCommitDirtyMain turns a dirty main checkout into a janitorial commit
// before merging. Uncommitted changes the merge touches fail the whole merge
// ("Your local changes would be overwritten") — observed 2026-08-19 when 9
// legacy-era files in srv-smart-engine blocked every integration and
// gate-verified work piled up in quarantine while beads closed as completed.
// Committing first converts that hard failure into an ordinary two-parent
// merge. Same-hunk content conflicts can still conflict — those quarantine.
func autoCommitDirtyMain(projectPath string) {
	out, err := gitOut(projectPath, "status", "--porcelain")
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	if _, err := gitOut(projectPath, "add", "-A"); err != nil {
		log.Printf("[Worktree] auto-commit dirty main: add failed (continuing): %v", err)
		return
	}
	if _, err := gitOut(projectPath, "commit", "-m", "wip: hydra auto-commit of uncommitted main-checkout changes before integration"); err != nil {
		log.Printf("[Worktree] auto-commit dirty main: commit failed (continuing): %v", err)
		return
	}
	log.Printf("[Worktree] auto-committed dirty main checkout in %s before integration", projectPath)
}
