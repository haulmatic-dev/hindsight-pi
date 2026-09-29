package core

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// ResolveIntegrationBase returns the deterministic integration base branch for
// a project, plus a source label ("config" | "origin-default") for logs.
// Precedence:
//  1. the per-project integration_branch config knob, when set;
//  2. the origin default branch (git symbolic-ref refs/remotes/origin/HEAD,
//     falling back to refs/remotes/origin/development then main);
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
func originDefaultBranch(projectPath string) string {
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
