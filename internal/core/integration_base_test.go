package core

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// setupOriginClone creates a bare origin with a development branch and a
// clone of it. Returns the clone path.
func setupOriginClone(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bare := dir + "/origin.git"
	seed := dir + "/seed"
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run(dir, "init", "--bare", "-b", "development", bare)
	run(dir, "init", "-b", "development", seed)
	run(seed, "config", "user.email", "t@t")
	run(seed, "config", "user.name", "t")
	seedRun := func(args ...string) { run(seed, args...) }
	seedRun("commit", "--allow-empty", "-m", "init")
	seedRun("remote", "add", "origin", bare)
	seedRun("push", "-u", "origin", "development")
	clone := dir + "/clone"
	run(dir, "clone", bare, clone)
	run(clone, "config", "user.email", "t@t")
	run(clone, "config", "user.name", "t")
	// Park the clone on an unrelated branch — the incident state.
	run(clone, "checkout", "-b", "feat/parked")
	return clone
}

// TestResolveIntegrationBase_OriginDefaultDespiteParkedCheckout: checkout
// parked on an unrelated branch → resolver returns the origin default
// (development), never the checkout branch.
func TestResolveIntegrationBase_OriginDefaultDespiteParkedCheckout(t *testing.T) {
	clone := setupOriginClone(t)
	base, source, err := ResolveIntegrationBase(nil, clone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "development" {
		t.Fatalf("base = %q, want development", base)
	}
	if source != "origin-default" {
		t.Fatalf("source = %q, want origin-default", source)
	}
	if cur := currentBranch(clone); cur == "development" {
		t.Fatalf("test setup broken: checkout unexpectedly on development")
	}
}

// TestResolveIntegrationBase_ConfigKnobPrecedence: integration_branch config
// knob wins over the origin default.
func TestResolveIntegrationBase_ConfigKnobPrecedence(t *testing.T) {
	clone := setupOriginClone(t)
	cfg := &Config{Projects: []ProjectConfig{{
		Name:              "p",
		Path:              clone,
		IntegrationBranch: "release/integration",
	}}}
	base, source, err := ResolveIntegrationBase(cfg, clone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "release/integration" || source != "config" {
		t.Fatalf("got %q/%q, want release/integration/config", base, source)
	}
}

// TestResolveIntegrationBase_FailClosed: no origin refs and no config knob →
// error, never the checkout's current branch.
func TestResolveIntegrationBase_FailClosed(t *testing.T) {
	dir := t.TempDir()
	repo := dir + "/repo"
	run := func(args ...string) {
		t.Helper()
		os.MkdirAll(repo, 0o755) // git -C fails if dir missing (init creates it, but -C runs first)
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "development", repo)
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("commit", "--allow-empty", "-m", "init")
	// A remote is configured but has NO branches: origin default is
	// unresolvable → fail-closed (never the checkout's current branch).
	run("remote", "add", "origin", dir+"/nonexistent.git")
	_, _, err := ResolveIntegrationBase(nil, repo)
	if err == nil {
		t.Fatalf("expected fail-closed error")
	}
	if !strings.Contains(err.Error(), "refusing to use the checkout's current branch") {
		t.Fatalf("error missing fail-closed contract text: %v", err)
	}
}

// TestResolveIntegrationBase_SymbolicRefHead: when origin/HEAD is set, it
// wins over the development/main fallback.
func TestResolveIntegrationBase_SymbolicRefHead(t *testing.T) {
	clone := setupOriginClone(t)
	// main does not exist here; set origin/HEAD to development explicitly and
	// also create a decoy origin/development alias? origin/HEAD already
	// points at development after clone; instead repoint at a custom branch.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", clone}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("push", "origin", "HEAD:refs/heads/trunk")
	run("fetch", "origin")
	// Remote truth: point the bare origin's HEAD at trunk too (with
	// remote-aware resolution the remote advertisement now wins).
	originURL, err0 := exec.Command("git", "-C", clone, "remote", "get-url", "origin").Output()
	if err0 != nil {
		t.Fatalf("get-url: %v", err0)
	}
	if out, err := exec.Command("git", "--git-dir", strings.TrimSpace(string(originURL)), "symbolic-ref", "HEAD", "refs/heads/trunk").CombinedOutput(); err != nil {
		t.Fatalf("set remote HEAD: %v: %s", err, out)
	}
	run("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	base, _, err := ResolveIntegrationBase(nil, clone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "trunk" {
		t.Fatalf("base = %q, want trunk", base)
	}
}

// TestResolveIntegrationBase_RemoteTruthOverStaleLocalSymref: the local
// refs/remotes/origin/HEAD symref points at a stale branch while the remote
// advertises a different default (fms incident: local said main, remote
// default was development). ls-remote must win.
func TestResolveIntegrationBase_RemoteTruthOverStaleLocalSymref(t *testing.T) {
	clone := setupOriginClone(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", clone}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Make the remote's default main (GitHub-side default change) while the
	// clone's local symref stays stale at development.
	run("push", "origin", "HEAD:refs/heads/main")
	originURL := func() string {
		out, err := exec.Command("git", "-C", clone, "remote", "get-url", "origin").Output()
		if err != nil {
			t.Fatalf("get-url: %v", err)
		}
		return strings.TrimSpace(string(out))
	}()
	// Repoint the remote's HEAD to main.
	cmd := exec.Command("git", "--git-dir", originURL, "symbolic-ref", "HEAD", "refs/heads/main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set remote HEAD: %v: %s", err, out)
	}
	// Stale local symref: clone still says development.
	base, _, err := ResolveIntegrationBase(nil, clone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "main" {
		t.Fatalf("base = %q, want main (remote truth must beat stale local symref)", base)
	}
}

// TestResolveIntegrationBase_RemoteUnreachableFallsBackToLocalSymref: a
// hung/unreachable remote must not break resolution — the local symref is
// the fallback (fail-open).
func TestResolveIntegrationBase_RemoteUnreachableFallsBackToLocalSymref(t *testing.T) {
	clone := setupOriginClone(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", clone}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/development")
	run("remote", "set-url", "origin", "/nonexistent/unreachable.git")
	base, _, err := ResolveIntegrationBase(nil, clone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "development" {
		t.Fatalf("base = %q, want development via local symref fallback", base)
	}
}

// TestResolveIntegrationBase_ReconcilesStaleLocalSymref: when ls-remote
// reports a default branch different from the stale local
// refs/remotes/origin/HEAD, the resolver opportunistically runs
// `git remote set-head origin <branch>` so the local state heals.
func TestResolveIntegrationBase_ReconcilesStaleLocalSymref(t *testing.T) {
	clone := setupOriginClone(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", clone}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("push", "origin", "HEAD:refs/heads/main")
	originURL := func() string {
		out, err := exec.Command("git", "-C", clone, "remote", "get-url", "origin").Output()
		if err != nil {
			t.Fatalf("get-url: %v", err)
		}
		return strings.TrimSpace(string(out))
	}()
	if out, err := exec.Command("git", "--git-dir", originURL, "symbolic-ref", "HEAD", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("set remote HEAD: %v: %s", err, out)
	}
	// Force the local symref stale: origin/HEAD -> development while the
	// remote advertises main.
	run("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/development")
	base, _, err := ResolveIntegrationBase(nil, clone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "main" {
		t.Fatalf("base = %q, want main", base)
	}
	if out, err := exec.Command("git", "-C", clone, "symbolic-ref", "refs/remotes/origin/HEAD").Output(); err != nil || strings.TrimSpace(string(out)) != "refs/remotes/origin/main" {
		t.Fatalf("local symref not reconciled: %q err=%v", strings.TrimSpace(string(out)), err)
	}
}
