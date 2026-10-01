package core

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// ResolveIntegrationBase returns the deterministic integration base branch for
// a project, plus a source label ("config" | "origin-default") for logs.
// Precedence:
//  1. the per-project integration_branch config knob, when set;
//  2. the origin default branch (remote truth via `git ls-remote --symref
//     origin HEAD`, falling back to the local refs/remotes/origin/HEAD
//     symref, then refs/remotes/origin/development then main);
//  3. for repos with NO git remote (local-only mode, no-remote local-merge
//     fallback), the checkout's current branch — with a remote present that
//     state is incidental operator state (telematics incident 2026-09-29: a
//     checkout parked on feat/intergration-test-plan silently redirected
//     every PR to that stale branch) and is an error instead.
func ResolveIntegrationBase(cfg *Config, projectPath string) (string, string, error) {
	if b := integrationBranchFor(cfg, projectPath); b != "" {
		return b, "config", nil
	}
	if b := originDefaultBranch(projectPath); b != "" {
		return b, "origin-default", nil
	}
	// Not a git repository at all (config fixtures, fake paths): no base can
	// be derived; return empty WITHOUT error so legacy empty-base handling
	// downstream applies. Erroring here would break every local-only fixture.
	if !isGitRepo(projectPath) {
		return "", "no-repo", nil
	}
	if !hasGitRemote(projectPath) {
		if b := currentBranch(projectPath); b != "" {
			return b, "checkout-local-only", nil
		}
		return "", "", fmt.Errorf("integration base unresolved for %s: no integration_branch config, no origin default branch, and no local branch", projectPath)
	}
	return "", "", fmt.Errorf("integration base unresolved for %s: no integration_branch config and no origin default branch (symbolic-ref + development/main fallback all failed); refusing to use the checkout's current branch", projectPath)
}

// integrationBranchFor scans the configured projects for projectPath and
// returns its integration_branch knob, or "" when unset/not found.
func integrationBranchFor(cfg *Config, projectPath string) string {
	if cfg == nil || projectPath == "" {
		return ""
	}
	for _, p := range cfg.Projects {
		if p.Path == projectPath && p.IntegrationBranch != "" {
			return p.IntegrationBranch
		}
		for _, r := range p.Repos {
			if r.Path == projectPath && r.IntegrationBranch != "" {
				return r.IntegrationBranch
			}
		}
	}
	return ""
}

// originDefaultBranch resolves the origin default branch, or "" when it
// cannot be determined.
//
// Remote truth first: `git ls-remote --symref origin HEAD` asks the remote
// which branch it advertises as default. The local symref
// refs/remotes/origin/HEAD goes stale when the remote's default branch
// changes after the clone (fms incident 2026-09-30: clone recorded main,
// GitHub default had moved to development — PRs targeted the dead branch).
// ls-remote is read-only (no set-head mutation) and must never gate: any
// failure (offline, auth, hung remote) falls back to the local symref and
// then the development/main candidates.
func originDefaultBranch(projectPath string) string {
	if b := remoteSymrefDefault(projectPath); b != "" {
		return b
	}
	out, err := exec.Command("git", "-C", projectPath, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		ref = strings.TrimPrefix(ref, "refs/remotes/")
		if strings.HasPrefix(ref, "origin/") {
			return strings.TrimPrefix(ref, "origin/")
		}
	}
	for _, candidate := range []string{"development", "main"} {
		if err := exec.Command("git", "-C", projectPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+candidate).Run(); err == nil {
			return candidate
		}
	}
	return ""
}

// remoteSymrefDefault resolves the default branch via `git ls-remote --symref
// origin HEAD` and returns its bare branch name, or "" on any failure
// (fail-open: caller falls back to local state).
func remoteSymrefDefault(projectPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", projectPath, "ls-remote", "--symref", "origin", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[IntegrationBase] ls-remote --symref origin HEAD failed in %s (%v) — falling back to local refs/remotes/origin/HEAD", projectPath, err)
		return ""
	}
	// First line: "ref: refs/heads/<default>\tHEAD"
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref: ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref := strings.TrimPrefix(fields[1], "refs/heads/")
		if ref != "" {
			// Opportunistic self-heal: reconcile the local symref with the
			// remote's answer so the stale state does not persist. Best-effort
			// only — resolution already succeeded without it.
			if out, err := exec.Command("git", "-C", projectPath, "symbolic-ref", "refs/remotes/origin/HEAD").Output(); err == nil {
				if local := strings.TrimSpace(string(out)); local != "refs/remotes/origin/"+ref {
					if out2, err2 := exec.Command("git", "-C", projectPath, "remote", "set-head", "origin", ref).CombinedOutput(); err2 != nil {
						log.Printf("[IntegrationBase] could not reconcile stale refs/remotes/origin/HEAD in %s: %v: %s", projectPath, err2, out2)
					} else {
						log.Printf("[IntegrationBase] reconciled stale refs/remotes/origin/HEAD in %s: %s -> %s", projectPath, local, ref)
					}
				}
			}
			return ref
		}
	}
	return ""
}

// logIntegrationBaseMismatch emits the incident-signature observability line:
// the resolved base differs from the main checkout's current branch. Never
// gates — purely informational.
func logIntegrationBaseMismatch(projectPath, base, source string) {
	if cur := currentBranch(projectPath); cur != "" && cur != base {
		log.Printf("[IntegrationBase] resolved integration base %q (source: %s) differs from checkout branch %q — using %q", base, source, cur, base)
	}
}

// isGitRepo reports whether projectPath is inside a git repository.
func isGitRepo(projectPath string) bool {
	_, err := gitOut(projectPath, "rev-parse", "--git-dir")
	return err == nil
}
