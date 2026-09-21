package core

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	mu        sync.Mutex // serialize git worktree ops (shared repo, single process)
	eventSink *EventSink // optional: emits worktree-level typed events
}

// NewWorktreeManager creates a worktree manager.
func NewWorktreeManager() *WorktreeManager { return &WorktreeManager{} }

// SetEventSink wires an event sink for worktree-level typed events. Nil-safe.
func (m *WorktreeManager) SetEventSink(sink *EventSink) {
	m.eventSink = sink
}

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

// versionedKeySuffix is the branch-name suffix a versioned worktree branch
// carries: hydra/wt-<key>-v2, -v3, ... (webapp-tc5 incident, 2026-09-16).
const versionedKeySuffix = "-v"

// splitVersionedKey strips a -vN version suffix from a worktree key, returning
// the base key the bead and worktree directory actually use. "webapp-tc5-v2"
// is branch-name residue of key "webapp-tc5" — the bead, the worktree dir,
// and every registry row still key it as "webapp-tc5". Returns ("", false)
// when no version suffix is present.
func splitVersionedKey(key string) (string, bool) {
	i := strings.LastIndex(key, versionedKeySuffix)
	if i < 0 {
		return "", false
	}
	suffix := key[i+len(versionedKeySuffix):]
	if suffix == "" {
		return "", false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	if n, err := strconv.Atoi(suffix); err != nil || n < 2 {
		return "", false
	}
	return key[:i], true
}

// newestVersionedBranch returns the highest-numbered existing
// hydra/wt-<key>-vN branch, or "" when none exists. Used as the recorded-key
// fallback at chain completion: the versioned branch that was current when
// the chain finished is the newest one.
func newestVersionedBranch(m *WorktreeManager, projectPath, key string) string {
	prefix := m.branchName(key) + versionedKeySuffix
	out, err := gitOut(projectPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+prefix+"*")
	if err != nil {
		return ""
	}
	best, bestN := "", 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(line, prefix)); err == nil && n > bestN {
			best, bestN = line, n
		}
	}
	return best
}

// ResolveBranch returns the branch actually checked out in the key's
// worktree, or the key's canonical branch when no worktree exists. This is
// the recorded-key contract: after EnsureWorktree, ResolveBranch names the
// REAL branch (which may be a -vN versioned one), not the name a downstream
// consumer would guess from the key alone.
func (m *WorktreeManager) ResolveBranch(projectPath, key string) string {
	wtPath := m.worktreePath(projectPath, key)
	if isGitWorktree(wtPath) {
		if out, err := gitOut(wtPath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			if b := strings.TrimSpace(out); b != "HEAD" {
				return b
			}
		}
	}
	return m.branchName(key)
}

// WorktreeRecentlyActive reports whether the worktree for key has any write
// newer than floor (directory itself or any file, skipping dependency/build
// trees). A missing directory returns false — there is nothing to protect.
// Used as the reconciler's age floor: actively-written sandboxes are never
// integrated or removed, independent of registry/chain/bead correctness.
func (m *WorktreeManager) WorktreeRecentlyActive(projectPath, key string, floor time.Duration) bool {
	wtPath := m.worktreePath(projectPath, key)
	info, err := os.Stat(wtPath)
	if err != nil || !info.IsDir() {
		return false
	}
	cutoff := time.Now().Add(-floor)
	if info.ModTime().After(cutoff) {
		return true
	}
	recent := false
	_ = filepath.WalkDir(wtPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || recent {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", ".angular":
				return filepath.SkipDir
			}
			return nil
		}
		if fi, fierr := d.Info(); fierr == nil && fi.ModTime().After(cutoff) {
			recent = true
			return filepath.SkipAll
		}
		return nil
	})
	return recent
}

// changelogUnionEntry marks CHANGELOG.md for git's built-in union merge
// driver: on conflict both sides' lines survive. Every worker appends its
// changelog entry at the same anchor per the documentation doctrine, so any
// two wt-branches from a parallel batch conflict there on integrate — the
// close then fails and the bead reopens in a loop (webapp isk/hrx/lbu/1oi/cpm,
// 2026-09-16). Worst union outcome is a rare duplicated entry, which a
// changelog tolerates; a close-reopen loop does not get tolerated.
const changelogUnionEntry = "CHANGELOG.md merge=union"

// ensureChangelogUnionDriver idempotently enables the union merge driver for
// CHANGELOG.md in the repo-local (uncommitted) .git/info/attributes, which
// applies to every worktree and clone-local merge of that repo. Fail-open: a
// repo we cannot inspect still gets its worktree.
func ensureChangelogUnionDriver(projectPath string) {
	commonDir, err := gitOut(projectPath, "rev-parse", "--git-common-dir")
	if err != nil {
		log.Printf("[Worktree] changelog union driver: rev-parse failed for %s: %v", projectPath, err)
		return
	}
	dir := strings.TrimSpace(commonDir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(projectPath, dir)
	}
	infoDir := filepath.Join(dir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		log.Printf("[Worktree] changelog union driver: mkdir %s: %v", infoDir, err)
		return
	}
	attrPath := filepath.Join(infoDir, "attributes")
	if data, err := os.ReadFile(attrPath); err == nil && strings.Contains(string(data), changelogUnionEntry) {
		return
	}
	f, err := os.OpenFile(attrPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[Worktree] changelog union driver: open %s: %v", attrPath, err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(changelogUnionEntry + "\n"); err != nil {
		log.Printf("[Worktree] changelog union driver: write %s: %v", attrPath, err)
		return
	}
	log.Printf("[Worktree] CHANGELOG.md union merge driver enabled for %s", projectPath)
}

// repoToplevel returns the absolute root of the git working tree that owns
// path, or an error if path is not inside a git repository.
func repoToplevel(path string) (string, error) {
	out, err := gitOut(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Abs(strings.TrimSpace(out))
}

// pathsEqual reports whether two filesystem paths denote the same directory
// after cleaning and resolving to an absolute path.
func pathsEqual(a, b string) bool {
	a, err := filepath.Abs(filepath.Clean(a))
	if err != nil {
		return false
	}
	b, err = filepath.Abs(filepath.Clean(b))
	if err != nil {
		return false
	}
	return a == b
}

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
// After the worktree exists, sibling `file:` deps are provisioned into the
// repo's worktree root (see provisionSiblingDeps).
func (m *WorktreeManager) EnsureWorktree(projectPath, key string) (string, error) {
	wtPath, err := m.ensureWorktreeLocked(projectPath, key)
	if err != nil {
		return "", err
	}
	m.provisionSiblingDeps(projectPath)
	return wtPath, nil
}

func (m *WorktreeManager) ensureWorktreeLocked(projectPath, key string) (string, error) {
	if projectPath == "" {
		return "", fmt.Errorf("refusing to create worktree %q with empty project path (would worktree the daemon cwd — 1xs7z wrong-repo incident, 2026-08-23)", key)
	}

	// Wrong-repo guard (telematics incident, 2026-09-21): git commands in a
	// directory that is not a repo root walk up to the nearest parent repo
	// (/opt/dev-tools monorepo). Verify the resolved toplevel matches the
	// project path before any worktree operation touches the repo.
	if top, err := repoToplevel(projectPath); err != nil {
		return "", fmt.Errorf("worktree guard for %s: project path %q is not a git repository: %w", key, projectPath, err)
	} else if !pathsEqual(top, projectPath) {
		detail := fmt.Sprintf("worktree guard for %s: git toplevel %q does not match project path %q", key, top, projectPath)
		if m.eventSink != nil {
			m.eventSink.Emit(Event{
				Type:   "worktree_refused",
				Reason: "toplevel_mismatch",
				Detail: detail,
				Fields: map[string]any{
					"project_path": projectPath,
					"toplevel":     top,
					"key":          key,
				},
			})
		}
		return "", fmt.Errorf("%s", detail)
	}

	ensureChangelogUnionDriver(projectPath)
	m.mu.Lock()
	defer m.mu.Unlock()

	wtPath := m.worktreePath(projectPath, key)
	branch := m.branchName(key)
	base := m.resolveBase(projectPath)

	// Something already occupies the target path. Distinguish a valid chain
	// worktree of this project from a broken symlink/empty dir/junk path, and
	// refuse to delete a valid worktree that belongs to a different repository.
	if _, err := os.Lstat(wtPath); err == nil {
		if isOurWorktree(projectPath, wtPath) {
			// Wrong branch checked out: a worktree sitting on the campaign
			// branch lets the worker commit straight onto it, the wt-branch
			// never exists, and every close then fails integration with
			// "not something we can merge" in a close-reopen loop (webapp-9ah,
			// 2026-09-16 parallel spawn storm). Repair only when nothing
			// unintegrated would be stranded; otherwise fail closed.
			if wtBranch, berr := gitOut(wtPath, "rev-parse", "--abbrev-ref", "HEAD"); berr == nil && strings.TrimSpace(wtBranch) != branch {
				head, _ := gitOut(wtPath, "rev-parse", "HEAD")
				if !isAncestorOf(projectPath, strings.TrimSpace(head), "HEAD") {
					return "", fmt.Errorf("worktree %s on wrong branch %s (want %s) with unintegrated commits at %s — refusing to touch, manual inspection required", wtPath, strings.TrimSpace(wtBranch), branch, strings.TrimSpace(head))
				}
				if _, cerr := gitOut(wtPath, "checkout", "-B", branch, base); cerr != nil {
					return "", fmt.Errorf("worktree %s branch repair (checkout -B %s %s): %w", wtPath, branch, base, cerr)
				}
				log.Printf("[Worktree] repaired %s: was on %s, now on %s at %s", wtPath, strings.TrimSpace(wtBranch), branch, base)
				return wtPath, nil
			}
			// Only reset a clean worktree whose HEAD is behind/ahead of the current
			// base. A dirty worktree contains in-flight worker output and must be
			// returned as-is (the caller will commit/integrate it).
			baseCommit, _ := gitOut(projectPath, "rev-parse", base)
			wtHead, _ := gitOut(wtPath, "rev-parse", "HEAD")
			if strings.TrimSpace(wtHead) != strings.TrimSpace(baseCommit) && worktreeClean(wtPath) {
				// Reuse-reset ONLY when the worktree tip is already contained in
				// the working branch. The unguarded reset rewound branches that
				// held gate-verified worker commits (f6269c0/d61ae5d hydra-xnx,
				// bf4e468d 2026-09-05/06): the integrate then silently no-opped
				// ("Already up to date"), the push verified equal, the bead
				// closed, and the work dangled unreferenced.
				if isAncestorOf(projectPath, strings.TrimSpace(wtHead), "HEAD") {
					if err := resetWorktree(wtPath, base); err != nil {
						return "", fmt.Errorf("worktree reuse reset %s: %w", wtPath, err)
					}
					log.Printf("[Worktree] recovered stale path %s (reuse)", wtPath)
				} else {
					log.Printf("[Worktree] keeping %s: unintegrated commits at %s (reuse-reset skipped)", wtPath, strings.TrimSpace(wtHead))
				}
			}
			return wtPath, nil
		}
		if isForeignWorktree(wtPath) {
			return "", fmt.Errorf("refusing to remove existing path %s: it is a valid git worktree of a different repository", wtPath)
		}

		// Broken symlink, empty directory, or otherwise unusable path: prune
		// stale git worktree registrations once, then remove the path and recreate.
		if _, err := gitOut(projectPath, "worktree", "prune"); err != nil {
			log.Printf("[Worktree] prune before recovery of %s: %v", wtPath, err)
		}
		if err := os.RemoveAll(wtPath); err != nil {
			return "", fmt.Errorf("remove stale worktree path %s: %w", wtPath, err)
		}
		log.Printf("[Worktree] recovered stale path %s (prune+recreate)", wtPath)
	} else if !os.IsNotExist(err) {
		return "", err
	}

	if err := os.MkdirAll(worktreeRoot(projectPath), 0o755); err != nil {
		return "", err
	}

	if branchExists(projectPath, branch) {
		if _, err := gitOut(projectPath, "worktree", "add", wtPath, branch); err != nil {
			return "", fmt.Errorf("worktree add (existing branch %s): %w", branch, err)
		}
	} else {
		if _, err := gitOut(projectPath, "worktree", "add", "-b", branch, wtPath, base); err != nil {
			return "", fmt.Errorf("worktree add %s off %s: %w", branch, base, err)
		}
	}

	// A stale branch may carry old commits; reset the (possibly reattached)
	// worktree to the current base so the chain starts from a clean, known
	// HEAD — but ONLY when those commits are already integrated into the
	// working branch. The unguarded reset orphaned gate-verified worker
	// commits on every respawn after worktree cleanup (srv af7v7/udasa/
	// 80ro0/q8awv 2026-09-08: commits dangled, worktree matched base,
	// verified-close rejected empty-diff, beads wrongfully escalated).
	reattachedTip, terr := gitOut(wtPath, "rev-parse", "HEAD")
	if terr == nil && !isAncestorOf(projectPath, strings.TrimSpace(reattachedTip), "HEAD") {
		log.Printf("[Worktree] keeping %s: unintegrated commits at %s (post-add reset skipped); branch %s stays the chain's identity — downstream lookups must use the recorded key", wtPath, strings.TrimSpace(reattachedTip), branch)
		return wtPath, nil
	}
	if err := resetWorktree(wtPath, base); err != nil {
		return "", fmt.Errorf("worktree reset after add %s: %w", wtPath, err)
	}
	return wtPath, nil
}

// provisionSiblingDeps materializes sibling `file:` dependencies declared in
// the project's package.json as symlinks at the repo's worktree root (the
// directory shared by all bead worktrees of the project). srv-telematics
// incident 2026-09-21: npm install turns `file:../lib-nestjs` into
// node_modules/@haulmatic/common -> ../../../lib-nestjs, which resolves
// against the worktree, where the sibling does not exist — preflight then
// fails with "dependency check failed after install: 1 broken symlink(s)"
// and the bead loops on spawn failure. The symlink therefore belongs at the
// worktree root, one level above each bead worktree. Idempotent: a correct
// existing symlink is kept; a wrong or dangling one is replaced. Fail-open:
// a sibling that does not exist on disk is logged and skipped, never fatal.
func (m *WorktreeManager) provisionSiblingDeps(projectPath string) {
	data, err := os.ReadFile(filepath.Join(projectPath, "package.json"))
	if err != nil {
		return // not a node project (or unreadable manifest) — nothing to do
	}
	root := worktreeRoot(projectPath)
	for _, dep := range siblingFileDeps(data) {
		target := dep
		if !filepath.IsAbs(target) {
			target = filepath.Join(projectPath, target)
		}
		target = filepath.Clean(target)
		// In-repo file: deps (file:./packages/x) ship inside the checkout and
		// resolve within the worktree already — only out-of-repo siblings need
		// provisioning at the worktree root.
		if rel, rerr := filepath.Rel(projectPath, target); rerr != nil || rel == "." ||
			(rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			continue
		}
		info, err := os.Stat(target)
		if err != nil || !info.IsDir() {
			log.Printf("[Worktree] sibling file: dep %s: target %s not found — skipping", dep, target)
			continue
		}
		link := filepath.Join(root, filepath.Base(target))
		if fi, err := os.Lstat(link); err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				if cur, err := os.Readlink(link); err == nil && pathsEqual(cur, target) {
					continue // already provisioned, correct target
				}
			}
			if err := os.RemoveAll(link); err != nil {
				log.Printf("[Worktree] sibling file: dep %s: replace stale %s: %v", dep, link, err)
				continue
			}
		} else if !os.IsNotExist(err) {
			continue
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			log.Printf("[Worktree] sibling file: dep %s: mkdir %s: %v", dep, root, err)
			continue
		}
		if err := os.Symlink(target, link); err != nil {
			log.Printf("[Worktree] sibling file: dep %s: symlink %s -> %s: %v", dep, link, target, err)
			continue
		}
		log.Printf("[Worktree] provisioned sibling file: dep %s: %s -> %s", dep, link, target)
	}
}

// stagedConflictMarkerFiles lists staged files whose added lines contain git
// merge-conflict markers (<<<<<<< / ======= / >>>>>>> at line start). Janitorial
// auto-commits have committed marker states into git 3+ times across repos
// (846bdd1 mid-merge capture, 9f74f940 unparseable package.json) — a commit
// that bakes markers in poisons every gate downstream.
func stagedConflictMarkerFiles(dir string) []string {
	out, err := gitOut(dir, "diff", "--cached", "--name-only", "-G", "^<<<<<<<|^=======|^>>>>>>>")
	if err != nil {
		return nil
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files
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
	// Refuse to bake conflict markers into history — alarm and fail so the
	// shepherd quarantines instead of poisoning every downstream gate.
	if files := stagedConflictMarkerFiles(worktreePath); len(files) > 0 {
		log.Printf("[Worktree] REFUSING commit in %s: conflict markers in %s", worktreePath, strings.Join(files, ", "))
		return false, fmt.Errorf("conflict markers in %s — refusing to commit", strings.Join(files, ", "))
	}
	if _, err := gitOut(worktreePath, "commit", "--no-verify", "-m", message); err != nil {
		return false, fmt.Errorf("commit in %s: %w", worktreePath, err)
	}
	return true, nil
}

// Integrate merges the chain's branch into the working branch (in the main
// checkout) and verifies the merge actually landed, but does NOT remove the
// worktree or branch — the caller runs post-merge steps (push) first and
// tears down via Remove once everything is verified. On merge conflict it
// aborts the merge and errors, leaving the worktree in place.
//
// Fail-closed verification: after a successful `git merge` the worktree
// branch tip must be contained in HEAD (`git merge-base --is-ancestor`). A
// merge that exits 0 without containing the branch (e.g. an "Already up to
// date" no-op on a rewound branch) is treated as an integration failure, not
// a success — the exact swallow that logged "integrated singleton" while no
// merge commit existed (hydra-xnx, 2026-09-06).
func (m *WorktreeManager) Integrate(projectPath, key string) error {
	return m.IntegrateBranch(projectPath, key, m.branchName(key))
}

// IntegrateBranch is Integrate with an explicit branch name: the branch to
// merge is the RECORDED one (possibly a -vN versioned name), while the
// worktree directory and teardown key stay the caller's key.
func (m *WorktreeManager) IntegrateBranch(projectPath, key, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wtPath := m.worktreePath(projectPath, key)
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

	// Verify the effect, not the command: the branch tip must be contained in
	// HEAD now. Failure keeps worktree + branch and reports an error so the
	// caller never logs "integrated" over a no-op merge.
	if !isAncestorOf(projectPath, branch, "HEAD") {
		tip, _ := gitOut(projectPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
		head, _ := gitOut(projectPath, "rev-parse", "HEAD")
		return fmt.Errorf("integrate %s: merge verification failed: branch tip %s not contained in HEAD %s (worktree left at %s)",
			branch, strings.TrimSpace(tip), strings.TrimSpace(head), wtPath)
	}
	return nil
}

// IntegrateAndRemove merges the chain's branch into the working branch,
// verifies the merge landed, and removes the worktree + branch. It is the
// teardown-only variant for callers with no post-merge push step; the
// singleton close path uses Integrate + Remove so a rejected push still
// finds the worktree present.
func (m *WorktreeManager) IntegrateAndRemove(projectPath, key string) error {
	return m.IntegrateBranchAndRemove(projectPath, key, m.branchName(key))
}

// IntegrateBranchAndRemove is IntegrateAndRemove with an explicit branch
// name — the recorded-key path for versioned worktree branches.
func (m *WorktreeManager) IntegrateBranchAndRemove(projectPath, key, branch string) error {
	if err := m.IntegrateBranch(projectPath, key, branch); err != nil {
		return err
	}
	m.removeWorktreeAndBranchName(projectPath, key, branch)
	return nil
}

// removeWorktreeAndBranch tears down a chain's worktree + branch. Called by
// IntegrateAndRemove and Remove after their respective gates pass.
func (m *WorktreeManager) removeWorktreeAndBranch(projectPath, key string) {
	m.removeWorktreeAndBranchName(projectPath, key, m.branchName(key))
}

func (m *WorktreeManager) removeWorktreeAndBranchName(projectPath, key, branch string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wtPath := m.worktreePath(projectPath, key)
	if isGitWorktree(wtPath) {
		_, _ = gitOut(projectPath, "worktree", "remove", wtPath, "--force")
	}
	_, _ = gitOut(projectPath, "branch", "-D", branch)
	_, _ = gitOut(projectPath, "worktree", "prune")
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
	return m.QuarantineBranchAndRemove(projectPath, key, m.branchName(key))
}

// QuarantineBranchAndRemove is QuarantineAndRemove with an explicit branch
// name — the recorded-key path for versioned worktree branches.
func (m *WorktreeManager) QuarantineBranchAndRemove(projectPath, key, branch string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	wtPath := m.worktreePath(projectPath, key)
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
	m.removeWorktreeAndBranch(projectPath, key)
}

// CleanupStaleWorktrees removes per-chain worktrees that are older than
// maxAge and not owned by an active worker. Live runs accumulate one worktree
// per bead/chain and nothing removed them (hundreds observed after one run) —
// integration removes a worktree only when its chain completes cleanly, so
// failed/abandoned chains leak. Returns the number of worktrees removed.
//
// Fail-closed on in-flight work: a stale worktree whose branch carries commits
// not yet contained in the working branch, or that has uncommitted tracked
// changes, is PRESERVED (not removed) and a worktree-preserved-unintegrated
// event is emitted — the janitorial sweep wiped deliverables twice between
// verdict and verified-close (48bdq, 2026-09-06/07): "Integration commit
// b1b9b32df, specs wiped, jest missing. Repair both." The reconciler lands the
// preserved branch on its next sweep; the event makes the preservation
// visible instead of silent. The check fails open only when the source repo
// or git queries are unavailable (never wedge the sweep on a broken check).
//
// Staleness is judged by the worktree's .git file mtime (set at creation).
// The source repository (needed for `git worktree prune` and the unintegrated
// check) is recovered from the .git file's "gitdir: <src>/.git/worktrees/<name>"
// pointer.
func CleanupStaleWorktrees(maxAge time.Duration, activeKeys map[string]bool, events *EventSink) int {
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
			// Recent activity inside the worktree overrides .git-file staleness:
			// the pointer file's mtime is creation-time only and never reflects
			// later edits. A merge-conflict resolution, untracked worker output,
			// or an in-progress rebase all touch files without necessarily
			// dirtying TRACKED status (untracked files are deliberately ignored
			// by the dirty check below). Losing that work is never acceptable
			// (hydra-q3p reaped mid-merge on a dep-parked bead, 2026-09-12), so
			// anything touched within the staleness window is preserved outright.
			if worktreeRecentlyActive(wt, info.ModTime(), maxAge) {
				log.Printf("[WorktreeGC] preserving %s: file activity within %s (work in progress)", wt, maxAge)
				events.EmitRepo("", Event{Type: "worktree-preserved-unintegrated", Project: filepath.Base(projDir),
					Bead: e.Name(), Reason: "recent_activity",
					Detail: fmt.Sprintf("stale-looking worktree %s preserved: files touched within %s", wt, maxAge),
					Fields: map[string]any{"worktree": wt, "cause": "recent_activity"}})
				continue
			}
			if src := worktreeSourceRepo(gitFile); src != "" {
				if reason, preserve := unintegratedWorkReason(src, "hydra/wt-"+e.Name(), wt); preserve {
					log.Printf("[WorktreeGC] preserving %s: %s (reconciler will integrate)", wt, reason)
					events.EmitRepo(src, Event{Type: "worktree-preserved-unintegrated", Project: filepath.Base(src),
						Bead: e.Name(), Reason: "unintegrated_work",
						Detail: fmt.Sprintf("stale worktree %s preserved: %s", wt, reason),
						Fields: map[string]any{"worktree": wt, "branch": "hydra/wt-" + e.Name(), "cause": reason}})
					continue
				}
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

// unintegratedWorkReason reports why a stale worktree must be preserved
// rather than GC'd: commits on its branch not contained in the working
// branch, or uncommitted tracked changes. Untracked files (node_modules,
// build output) are deliberately ignored — GC would otherwise never fire on
// any Node worktree. Returns ok=false when nothing provable is unintegrated,
// including when git queries fail (fail-open: a broken check must not wedge
// the janitorial sweep — only a definitive positive preserves).
func unintegratedWorkReason(src, branch, wtPath string) (reason string, ok bool) {
	if branchExists(src, branch) {
		if out, err := gitOut(src, "rev-list", "--count", "HEAD..refs/heads/"+branch); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil && n > 0 {
				return fmt.Sprintf("%d commit(s) on %s not in working branch", n, branch), true
			}
		}
	}
	if out, err := gitOut(wtPath, "status", "--porcelain", "--untracked-files=no"); err == nil && strings.TrimSpace(out) != "" {
		return "uncommitted tracked changes in worktree", true
	}
	return "", false
}

// worktreeRecentlyActive reports whether any regular file under wt (excluding
// the .git pointer file) was modified BOTH after the worktree was created
// (gitMtime, the .git pointer file's mtime — checkout files are written
// moments after it, so a creation-time margin excludes the pristine checkout)
// AND within maxAge of now. Directories are ignored: their mtimes change on
// transient operations and a lone directory holds no work. Never follows
// symlinks (WalkDir does not descend into them).
func worktreeRecentlyActive(wt string, gitMtime time.Time, maxAge time.Duration) bool {
	newest := gitMtime.Add(worktreeCheckoutMargin)
	cutoff := time.Now().Add(-maxAge)
	active := false
	_ = filepath.WalkDir(wt, func(path string, d fs.DirEntry, err error) error {
		if err != nil || active || d.IsDir() {
			return nil
		}
		if path == filepath.Join(wt, ".git") {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) && fi.ModTime().After(cutoff) {
			active = true
		}
		return nil
	})
	return active
}

// worktreeCheckoutMargin is the creation-time slack between the .git pointer
// file's mtime and the checkout files materialized by `git worktree add`.
// A file must be modified at least this long after creation to count as
// activity, so a pristine checkout never looks live.
const worktreeCheckoutMargin = 2 * time.Minute

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

// isAncestorOf reports whether ancestor is contained in descendant
// (`git merge-base --is-ancestor`). Exit 1 (not an ancestor) is a definitive
// false; any git error is also false — callers bias toward preservation on
// uncertainty (never reset/remove unverified work).
func isAncestorOf(repoPath, ancestor, descendant string) bool {
	if ancestor == "" || descendant == "" {
		return false
	}
	_, err := gitOut(repoPath, "merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

func (m *WorktreeManager) resolveBase(projectPath string) string {
	base, err := m.WorkingBranch(projectPath)
	if err != nil || base == "" || base == "HEAD" {
		return "HEAD"
	}
	return base
}

func resetWorktree(wtPath, base string) error {
	if _, err := gitOut(wtPath, "reset", "--hard", base); err != nil {
		return err
	}
	_, err := gitOut(wtPath, "clean", "-fd")
	return err
}

func isOurWorktree(projectPath, wtPath string) bool {
	if _, err := os.Stat(filepath.Join(wtPath, ".git")); err != nil {
		return false
	}
	// Identity via the common git dir: survives submodules (gitdir under
	// <super>/.git/modules/<name>) and linked-worktree redirection. The old
	// "/.git/worktrees/" marker parse false-positived on submodule layouts and
	// wedged every hydra spawn on 2026-08-24 (hydra-x9e: 67 retries).
	ours := gitCommonDir(projectPath)
	theirs := gitCommonDir(wtPath)
	return ours != "" && ours == theirs
}

// gitCommonDir resolves a checkout's shared git directory as an absolute path.
func gitCommonDir(path string) string {
	out, err := gitOut(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func isForeignWorktree(wtPath string) bool {
	if _, err := os.Stat(filepath.Join(wtPath, ".git")); err != nil {
		return false
	}
	_, err := gitOut(wtPath, "rev-parse", "--git-dir")
	return err == nil
}

func worktreeClean(wtPath string) bool {
	out, err := gitOut(wtPath, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) == ""
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
	// .beads is daemon machinery state, same exclusion as worktree commits.
	if _, err := gitOut(projectPath, "add", "-A", "--", ".", ":(exclude).beads"); err != nil {
		log.Printf("[Worktree] auto-commit dirty main: add failed (continuing): %v", err)
		return
	}
	// Skip + alarm rather than bake markers into the branch: marker states
	// committed by janitorial runs poisoned every gate downstream (846bdd1,
	// 9f74f940). Better a dirty main that fails the merge loudly than a
	// branch whose files are unparseable.
	if files := stagedConflictMarkerFiles(projectPath); len(files) > 0 {
		if _, err := gitOut(projectPath, "reset"); err != nil {
			log.Printf("[Worktree] auto-commit dirty main: unstage after marker refusal failed: %v", err)
		}
		log.Printf("[Worktree] ALARM auto-commit dirty main: refusing janitorial commit in %s — conflict markers in %s", projectPath, strings.Join(files, ", "))
		return
	}
	if _, err := gitOut(projectPath, "commit", "-m", "wip: hydra auto-commit of uncommitted main-checkout changes before integration"); err != nil {
		log.Printf("[Worktree] auto-commit dirty main: commit failed (continuing): %v", err)
		return
	}
	log.Printf("[Worktree] auto-committed dirty main checkout in %s before integration", projectPath)
}
