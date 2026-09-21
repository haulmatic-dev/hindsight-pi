package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// preflightSentinel marks a worktree whose dependencies were verified by
// Preflight. Content: hex sha256 of the lockfile that was verified, so a
// lockfile change invalidates the cache.
const preflightSentinel = ".hydra-preflight-ok"

// preflightInstallTimeout bounds one dependency install (npm ci on a large
// NestJS repo takes minutes; 632M node_modules observed).
const preflightInstallTimeout = 10 * time.Minute

// installRunner runs the dependency install and returns combined output.
// Replaceable in tests.
var installRunner = func(ctx context.Context, wtPath, manager string) (string, error) {
	var cmd *exec.Cmd
	switch manager {
	case "pnpm":
		cmd = exec.CommandContext(ctx, "pnpm", "install", "--frozen-lockfile")
	case "yarn":
		cmd = exec.CommandContext(ctx, "yarn", "install", "--frozen-lockfile")
	default:
		cmd = exec.CommandContext(ctx, "npm", "ci")
	}
	cmd.Dir = wtPath
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Preflight verifies a worktree's dependencies are installed and resolvable
// BEFORE a worker spawns into it (bead hydra-code-jwrh). Without it, workers
// burned their whole lifetime discovering missing node_modules or broken
// dependency symlinks (e.g. @haulmatic/common -> ../../../lib-nestjs) and died
// without completing. Fail-closed: an unrecoverable environment aborts the
// spawn so the bead stays open for retry instead of churning closed-failed.
//
// v1 scope: Node projects (package.json). Other stacks are a no-op.
func (m *WorktreeManager) Preflight(wtPath string) error {
	if !fileExists(filepath.Join(wtPath, "package.json")) {
		return nil
	}
	manager, lockHash := detectNodeManager(wtPath)

	if data, err := os.ReadFile(filepath.Join(wtPath, preflightSentinel)); err == nil &&
		strings.TrimSpace(string(data)) == lockHash {
		return nil // verified against this lockfile already
	}

	nm := filepath.Join(wtPath, "node_modules")
	if broken := brokenDepSymlinks(nm); dirExists(nm) && len(broken) == 0 {
		return writePreflightSentinel(wtPath, lockHash)
	}

	ctx, cancel := context.WithTimeout(context.Background(), preflightInstallTimeout)
	defer cancel()
	out, err := installRunner(ctx, wtPath, manager)
	if err != nil {
		return fmt.Errorf("dependency install failed (%s): %v: %s", manager, err, tailString(out, 400))
	}

	if broken := brokenDepSymlinks(nm); len(broken) > 0 {
		return fmt.Errorf("dependency check failed after install: %d broken symlink(s), e.g. %s",
			len(broken), broken[0])
	}
	return writePreflightSentinel(wtPath, lockHash)
}

// detectNodeManager picks the installer from the lockfile present and hashes
// it for the sentinel. Defaults to npm with an empty hash.
func detectNodeManager(wtPath string) (manager, lockHash string) {
	for _, lf := range []struct{ name, mgr string }{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"package-lock.json", "npm"},
	} {
		if data, err := os.ReadFile(filepath.Join(wtPath, lf.name)); err == nil {
			return lf.mgr, fmt.Sprintf("%x", sha256.Sum256(data))
		}
	}
	return "npm", ""
}

// brokenDepSymlinks lists symlinks under node_modules (top level and scoped
// @org/* level) whose targets do not resolve. node_modules layout is flat
// packages plus scoped dirs; deeper walks cost too much on 600M trees.
func brokenDepSymlinks(nm string) []string {
	var broken []string
	check := func(path string) {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return
		}
		if _, err := os.Stat(path); err != nil {
			broken = append(broken, path)
		}
	}
	entries, err := os.ReadDir(nm)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		p := filepath.Join(nm, e.Name())
		if strings.HasPrefix(e.Name(), "@") && e.IsDir() {
			if subs, err := os.ReadDir(p); err == nil {
				for _, s := range subs {
					check(filepath.Join(p, s.Name()))
				}
			}
			continue
		}
		check(p)
	}
	return broken
}

// siblingFileDeps extracts the `file:` dependency paths declared in a
// package.json manifest (dependencies and devDependencies). Used by
// provisionSiblingDeps to decide which sibling repos need symlinks at the
// repo's worktree root. Malformed manifests yield nothing (fail-open).
func siblingFileDeps(pkgJSON []byte) []string {
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(pkgJSON, &manifest); err != nil {
		return nil
	}
	var deps []string
	for _, set := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
		for _, v := range set {
			if p, ok := strings.CutPrefix(v, "file:"); ok && p != "" {
				deps = append(deps, p)
			}
		}
	}
	return deps
}

func writePreflightSentinel(wtPath, lockHash string) error {
	return os.WriteFile(filepath.Join(wtPath, preflightSentinel), []byte(lockHash+"\n"), 0o644)
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func tailString(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
