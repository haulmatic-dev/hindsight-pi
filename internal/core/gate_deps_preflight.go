package core

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Node dependency preflight close gate (companion to the executable-acceptance
// hard gate, FR-001 chain).
//
// Contract (pinned by gate_deps_preflight_test.go):
//
//   - a package manifest (package.json) present with no node_modules
//     directory: run the project's package install command in the worktree
//     BEFORE the verification command;
//   - no manifest: no install at all — the verification command runs
//     directly;
//   - node_modules already present: no install;
//   - an install that leaves the worktree dirty (tree-clean guard, e.g. a
//     non-empty `git status --porcelain`) is a failure: the verification
//     command must NOT run and an error must be returned;
//   - the verification command's output is returned verbatim (--- PASS lines
//     and all, nothing swallowed).

// gateDepsEnforceEnv turns the RED contract's loud skips into failures so the
// missing feature is provable: `GATE_DEPS_ENFORCE=1 go test ...`.
const gateDepsEnforceEnv = "GATE_DEPS_ENFORCE"

// depsPreflightImplemented is flipped to true by the GREEN phase; the
// contract tests skip loudly while it is false.
const depsPreflightImplemented = true

// gateDepsPreflight ensures Node dependencies are installed in worktreePath
// when needed (see the contract above), guards that the worktree stays clean
// after any install, then runs command via run and returns its output
// verbatim.
func gateDepsPreflight(worktreePath string, command string, run func(dir, command string) (string, error)) (string, error) {
	if depsInstallRequired(worktreePath) {
		if err := installNodeDeps(worktreePath, run); err != nil {
			return "", err
		}
	}
	return run(worktreePath, command)
}

// depsInstallRequired reports whether the worktree carries a package manifest
// without an installed node_modules directory — the one shape that needs a
// dependency install before the verification command can run.
func depsInstallRequired(worktreePath string) bool {
	return fileExists(filepath.Join(worktreePath, "package.json")) &&
		!dirExists(filepath.Join(worktreePath, "node_modules"))
}

// installNodeDeps runs the lockfile-detected package manager's install
// command in the worktree, then enforces the tree-clean guard: an install
// that leaves tracked files dirty is a failure, and the verification command
// must not run.
func installNodeDeps(worktreePath string, run func(dir, command string) (string, error)) error {
	manager, _ := detectNodeManager(worktreePath)
	if _, err := run(worktreePath, manager+" install"); err != nil {
		return fmt.Errorf("dependency install (%s install) failed in %s: %w", manager, worktreePath, err)
	}
	if dirty := worktreeDirtyAfterInstall(worktreePath); dirty != "" {
		return fmt.Errorf("dependency install left worktree %s dirty (tree-clean guard); verification command not run; git status --porcelain: %s", worktreePath, dirty)
	}
	return nil
}

// worktreeDirtyAfterInstall reports non-empty `git status --porcelain` output
// for dir (trimmed). Any git fault — including "not a git repository" — fails
// open to "" (clean): a non-repo has no tracked files an install could dirty,
// and a broken git must not block every Node close.
func worktreeDirtyAfterInstall(dir string) string {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
