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
		if _, ok := targetSet[clean]; ok {
			return true
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
func currentBranch(projectPath string) string {
	out, err := exec.Command("git", "-C", projectPath, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
