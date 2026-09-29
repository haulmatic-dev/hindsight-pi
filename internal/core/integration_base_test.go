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
	run("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	base, _, err := ResolveIntegrationBase(nil, clone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "trunk" {
		t.Fatalf("base = %q, want trunk", base)
	}
}
