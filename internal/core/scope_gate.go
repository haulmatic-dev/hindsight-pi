package core

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
)

// beadTypeShouldDeclareTargets reports whether a bead type is expected to
// declare Target Files because it writes code. It returns false for
// structural/aggregate types (epics, gates, verification, integration) that
// legitimately touch many or no files.
func beadTypeShouldDeclareTargets(beadType string) bool {
	switch strings.ToLower(strings.TrimSpace(beadType)) {
	case "fr", "fix", "implement", "green", "refactor", "task", "us-heavy":
		return true
	default:
		return false
	}
}

// isAllowlistedPath reports whether a changed path is in the standard
// documentation/dependency allowlist. These paths are always considered
// in-scope and should not trigger target-less warnings.
func isAllowlistedPath(path string) bool {
	base := filepath.Base(path)
	if base == "go.mod" || base == "go.sum" || base == "CHANGELOG.md" {
		return true
	}
	if path == "docs" || strings.HasPrefix(path, "docs"+string(filepath.Separator)) {
		return true
	}
	return false
}

// scopeViolations returns, in input order, every path in changed that is not
// allowed by the given target paths. A path is allowed when any of the
// following is true:
//   - it equals a target path (after stripping a ":symbol" suffix);
//   - it is a Go companion test file (_test.go) in the same directory as any
//     target;
//   - its base name is go.mod or go.sum.
func scopeViolations(changed []string, targets []string) []string {
	if len(targets) == 0 || len(changed) == 0 {
		return nil
	}

	targetSet, targetDirs := buildTargetSets(targets)

	var violations []string
	for _, c := range changed {
		clean := filepath.Clean(c)
		if !isAllowed(clean, targetSet, targetDirs) {
			violations = append(violations, c)
		}
	}
	return violations
}

// isAllowed reports whether a cleaned path is in-scope for the given targets.
func isAllowed(path string, targetSet map[string]struct{}, targetDirs map[string]struct{}) bool {
	if isAllowlistedPath(path) {
		return true
	}
	if _, ok := targetSet[path]; ok {
		return true
	}
	if strings.HasSuffix(path, "_test.go") {
		if _, ok := targetDirs[filepath.Dir(path)]; ok {
			return true
		}
	}
	return false
}

// buildTargetSets builds the cleaned target path set and the set of directories
// that contain targets. Reusing the same construction keeps scopeViolations
// and hasTargetChanges consistent and avoids duplicated cleaning logic.
func buildTargetSets(targets []string) (map[string]struct{}, map[string]struct{}) {
	targetSet := make(map[string]struct{}, len(targets))
	targetDirs := make(map[string]struct{}, len(targets))

	for _, t := range targets {
		t = stripSymbol(t)
		t = filepath.Clean(t)
		if t == "" || t == "." {
			continue
		}
		targetSet[t] = struct{}{}
		targetDirs[filepath.Dir(t)] = struct{}{}
	}
	return targetSet, targetDirs
}

// stripSymbol removes a trailing ":symbol" suffix from a target path, so
// targets like "internal/core/foo.go:Bar()" are compared on the file path only.
func stripSymbol(target string) string {
	if i := strings.Index(target, ":"); i >= 0 {
		return target[:i]
	}
	return target
}

// targetlessWarningPaths returns the changed paths that should be warned about
// when a bead has no declared Target Files. Allowlisted paths are filtered out
// so documentation/dependency edits stay noise-free.
func targetlessWarningPaths(changed []string) []string {
	var warnings []string
	for _, c := range changed {
		clean := filepath.Clean(c)
		if !isAllowlistedPath(clean) {
			warnings = append(warnings, c)
		}
	}
	return warnings
}

// hasTargetChanges reports whether any changed path satisfies the diff gate.
// When targets is empty, any non-empty changed slice satisfies it. When targets
// is non-empty, a path satisfies it only if it exactly matches a cleaned target
// (symbol suffixes are stripped). A nil changed slice means the diff could not
// be determined and is treated as changed (fail open).
func hasTargetChanges(changed, targets []string) bool {
	if changed == nil {
		return true
	}
	if len(targets) == 0 {
		return len(changed) > 0
	}
	targetSet, _ := buildTargetSets(targets)
	for _, c := range changed {
		clean := filepath.Clean(c)
		for t := range targetSet {
			// Exact match for file targets; path-boundary prefix match for
			// directory targets like "cmd/worker/" (hydra-2l9: directory
			// targets could never exact-match -> empty-diff reject loop).
			if clean == t || strings.HasPrefix(clean, t+"/") {
				return true
			}
		}
	}
	return false
}

// changedFiles returns the repo-relative paths of all uncommitted changes in
// repoPath by running "git status --porcelain". Untracked files are included,
// and for renames the new path is returned.
func changedFiles(repoPath string) ([]string, error) {
	if repoPath == "" {
		return nil, fmt.Errorf("repo path is empty")
	}

	cmd := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = repoPath

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status in %s: %w", repoPath, err)
	}

	return parseGitStatusPorcelain(string(out)), nil
}

// parseGitStatusPorcelain parses the output of "git status --porcelain" and
// returns the repo-relative paths of changed files, preserving order and using
// the new path for renames.
func parseGitStatusPorcelain(out string) []string {
	lines := strings.Split(out, "\n")
	files := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Renames (and copies) report "... old_path -> new_path".
		if sep := " -> "; strings.Contains(line, sep) {
			parts := strings.Split(line, sep)
			if len(parts) >= 2 {
				files = append(files, strings.TrimSpace(parts[len(parts)-1]))
			}
			continue
		}

		if len(line) < 2 {
			continue
		}
		// The first two characters are the status; the remainder is the path.
		path := strings.TrimSpace(line[2:])
		if path != "" {
			files = append(files, path)
		}
	}

	return files
}

// changedFilesWithBase returns uncommitted changes (git status) UNION
// committed-ahead-of-base changes (git diff base...HEAD). A worker that
// commits its own work in the chain worktree leaves a clean working tree —
// status-only produced empty-diff reject loops (65wa0: 24.9M tokens,
// 2026-08-21). Base resolution failure fails open (status-only result).
func changedFilesWithBase(repoPath, base string) ([]string, error) {
	changed, err := changedFiles(repoPath)
	if err != nil {
		return nil, err
	}
	if base == "" {
		return changed, nil
	}
	out, err := exec.Command("git", "-C", repoPath, "diff", "--name-only", base+"...HEAD").Output()
	if err != nil {
		log.Printf("[VerifiedClose] base diff failed for %s (status-only, fail open): %v", repoPath, err)
		return changed, nil
	}
	seen := make(map[string]bool, len(changed))
	for _, f := range changed {
		seen[f] = true
	}
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f != "" && !seen[f] {
			changed = append(changed, f)
			seen[f] = true
		}
	}
	return changed, nil
}

// currentBranch resolves the checked-out branch of a checkout (the base the
// chain worktree diverged from).
// committedChangesSinceBranchBase lists files changed by commits on the
// checkout's current branch since it diverged from its origin counterpart
// (merge-base HEAD origin/<branch>). This is the durable diff evidence for
// main-checkout-mode closes: uncommitted edits are revertible and must not
// satisfy the diff gate (2026-09-16 nipuna incident: close accepted on
// uncommitted edits, files reverted after acceptance, false close). Any
// failure (no branch, no upstream, git error) returns an error so callers
// can fail open.
func committedChangesSinceBranchBase(projectPath string) ([]string, error) {
	branch := currentBranch(projectPath)
	if branch == "" {
		return nil, fmt.Errorf("no current branch")
	}
	base, err := exec.Command("git", "-C", projectPath, "merge-base", "HEAD", "origin/"+branch).Output()
	if err != nil {
		return nil, err
	}
	baseStr := strings.TrimSpace(string(base))
	if baseStr == "" {
		return nil, fmt.Errorf("empty merge-base with origin/%s", branch)
	}
	out, err := exec.Command("git", "-C", projectPath, "diff", "--name-only", baseStr+"..HEAD").Output()
	if err != nil {
		return nil, err
	}
	// Empty result must be a NON-NIL slice: hasTargetChanges treats nil as
	// "unknown, fail open", which would let a no-commit branch pass the
	// diff gate — exactly the false-close class this helper exists to stop.
	files := []string{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// committedChangesSinceBase lists files changed by commits after the given
// base commit ("HEAD" resolves at call time). Empty result is a NON-NIL
// slice — nil means unknown/fail-open downstream.
func committedChangesSinceBase(projectPath, base string) ([]string, error) {
	if base == "HEAD" {
		out, err := exec.Command("git", "-C", projectPath, "rev-parse", "HEAD").Output()
		if err != nil {
			return nil, err
		}
		base = strings.TrimSpace(string(out))
	}
	out, err := exec.Command("git", "-C", projectPath, "diff", "--name-only", base+"..HEAD").Output()
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

func currentBranch(projectPath string) string {
	out, err := exec.Command("git", "-C", projectPath, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// changedFilesSinceMergeBase returns the repo-relative paths that changed on
// the main checkout between the merge base of baseBranch and chainBranch and
// HEAD. This detects work that was already integrated into the working branch
// before verified-close ran (5k33p class): the chain worktree may be clean,
// but the main checkout contains the bead's target changes since the chain
// diverged. Git errors are returned so the caller can decide whether to fail
// open or skip the check.
func changedFilesSinceMergeBase(projectPath, baseBranch, chainBranch string) ([]string, error) {
	if projectPath == "" || baseBranch == "" || chainBranch == "" {
		return nil, fmt.Errorf("changedFilesSinceMergeBase: projectPath, baseBranch, and chainBranch are required")
	}
	mb, err := exec.Command("git", "-C", projectPath, "merge-base", baseBranch, chainBranch).Output()
	if err != nil {
		return nil, fmt.Errorf("merge-base %s %s in %s: %w", baseBranch, chainBranch, projectPath, err)
	}
	mergeBase := strings.TrimSpace(string(mb))
	if mergeBase == "" {
		return nil, fmt.Errorf("merge-base returned empty for %s %s in %s", baseBranch, chainBranch, projectPath)
	}
	out, err := exec.Command("git", "-C", projectPath, "diff", "--name-only", mergeBase+"...HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("diff --name-only %s...HEAD in %s: %w", mergeBase, projectPath, err)
	}
	files := make([]string, 0)
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// branchIsAncestorOfMain reports whether branch is a strict ancestor of the
// main checkout's current HEAD. We require the branch tip to differ from
// HEAD so that an idle worktree that never diverged from main is not mistaken
// for an already-integrated chain.
func branchIsAncestorOfMain(projectPath, branch string) (bool, error) {
	if projectPath == "" || branch == "" {
		return false, fmt.Errorf("branchIsAncestorOfMain: projectPath and branch are required")
	}
	headOut, err := exec.Command("git", "-C", projectPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return false, fmt.Errorf("rev-parse HEAD in %s: %w", projectPath, err)
	}
	tipOut, err := exec.Command("git", "-C", projectPath, "rev-parse", branch).Output()
	if err != nil {
		return false, fmt.Errorf("rev-parse %s in %s: %w", branch, projectPath, err)
	}
	if strings.TrimSpace(string(headOut)) == strings.TrimSpace(string(tipOut)) {
		return false, nil
	}
	// merge-base --is-ancestor exits 0 when branch is an ancestor of HEAD,
	// exits 1 when it is not, and exits non-zero with stderr on error.
	cmd := exec.Command("git", "-C", projectPath, "merge-base", "--is-ancestor", branch, "HEAD")
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("merge-base --is-ancestor %s HEAD in %s: %w", branch, projectPath, err)
	}
	return true, nil
}
