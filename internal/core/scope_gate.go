package core

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// scopeViolations returns, in input order, every path in changed that is not
// allowed by the given target paths. A path is allowed when any of the
// following is true:
//   - it equals a target path (after stripping a ":symbol" suffix);
//   - it is a Go companion test file (_test.go) in the same directory as any
//     target;
//   - its base name is go.mod or go.sum.
func scopeViolations(changed []string, targets []string) []string {
	if len(targets) == 0 {
		// No targets means no scope limit; every change is allowed.
		return nil
	}
	if len(changed) == 0 {
		return nil
	}

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
	base := filepath.Base(path)
	if base == "go.mod" || base == "go.sum" {
		return true
	}
	// Documentation discipline (AGENTS.md): every change must update the
	// changelog and affected docs — those updates are always in-scope.
	if base == "CHANGELOG.md" || path == "CHANGELOG.md" ||
		path == "docs" || strings.HasPrefix(path, "docs"+string(filepath.Separator)) {
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

// stripSymbol removes a trailing ":symbol" suffix from a target path, so
// targets like "internal/core/foo.go:Bar()" are compared on the file path only.
func stripSymbol(target string) string {
	if i := strings.Index(target, ":"); i >= 0 {
		return target[:i]
	}
	return target
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
