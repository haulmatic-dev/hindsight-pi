package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED phase: the full-suite gate must move from IntegrateBranchAndRemove
// into IntegrateBranch so it runs on the always-present
// integrate-scratch-<key> post-merge worktree — gating BOTH the
// reconciler's branch-only re-integrate path and the singleton close
// fallback (which both route through IntegrateBranch). A typed error
// (ErrQuarantineSuiteGateBlocked) lets the quarantine reconciler emit
// quarantine_suite_gate_blocked without string sniffing.
//
// RED gate: skip loudly by default so the daemon lint gate stays green
// pre-GREEN; run with SUITE_GATE_MOVE_ENFORCE=1 to fail these tests and
// prove RED.

const suiteGateMoveEnforceEnv = "SUITE_GATE_MOVE_ENFORCE"

func suiteGateMoveRequireImpl(t *testing.T) {
	t.Helper()
	if os.Getenv(suiteGateMoveEnforceEnv) != "1" {
		t.Skipf("RED: suite-gate-in-IntegrateBranch contract not yet implemented (set %s=1 to fail)", suiteGateMoveEnforceEnv)
	}
}

// newGateFixtureRepo builds a temp git repo at a checkout branch ("main")
// with a committed base file, mirroring the integration_gate_test fixture.
func newGateFixtureRepo(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("needs git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := "cd " + dir + " && git " + strings.Join(args, " ")
		if out, err := execCommand(c); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "base")
	return dir
}

// commitInWorktree commits path "new.txt" with content y to the worktree.
func commitWorkFile(t *testing.T, wtPath, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wtPath, name), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	c := "cd " + wtPath + " && git add . && git commit -q -m work"
	if out, err := execCommand(c); err != nil {
		t.Fatalf("worktree commit: %v\n%s", err, out)
	}
}

// TestSuiteGate_IntegrateBranchGateRunsOnScratchAfterMerge pins AC1: the
// gate runs inside IntegrateBranch on the integrate-scratch-<key>
// worktree, AFTER the merge (merged content visible to the gate), and a
// gate failure surfaces as the typed ErrQuarantineSuiteGateBlocked while
// the merge contents never land on the checkout branch.
func TestSuiteGate_IntegrateBranchGateRunsOnScratchAfterMerge(t *testing.T) {
	suiteGateMoveRequireImpl(t)
	dir := newGateFixtureRepo(t)
	headBefore, _ := gitOut(dir, "rev-parse", "HEAD")

	m := NewWorktreeManager()
	var gateSeenPath string
	var sawMergedContent bool
	m.SuiteGate = func(projectPath, worktreePath, key string) error {
		gateSeenPath = worktreePath
		sawMergedContent = func(p string) bool {
			_, err := os.Stat(filepath.Join(p, "new.txt"))
			return err == nil
		}(worktreePath)
		return errors.New("suite failed: broken tests in post-merge tree")
	}

	wtPath, err := m.EnsureWorktree(dir, "scratch-gate-key")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	commitWorkFile(t, wtPath, "new.txt")

	err = m.IntegrateBranch(dir, "scratch-gate-key", m.branchName("scratch-gate-key"))
	if err == nil {
		t.Fatal("expected gate failure, got nil error")
	}
	if !errors.Is(err, ErrQuarantineSuiteGateBlocked) {
		t.Fatalf("gate failure must wrap ErrQuarantineSuiteGateBlocked, got %v", err)
	}
	if !strings.Contains(filepath.Base(gateSeenPath), "integrate-scratch-scratch-gate-key") {
		t.Fatalf("gate must run on the integrate-scratch worktree, got path %q", gateSeenPath)
	}
	if !sawMergedContent {
		t.Fatal("gate must run AFTER the merge — merged content must be visible in the scratch worktree")
	}
	// Fail-closed: nothing landed on the checkout branch.
	headAfter, _ := gitOut(dir, "rev-parse", "HEAD")
	if strings.TrimSpace(headAfter) != strings.TrimSpace(headBefore) {
		t.Fatalf("gate failure must keep the checkout branch at its old tip: before=%s after=%s", strings.TrimSpace(headBefore), strings.TrimSpace(headAfter))
	}
}

// TestSuiteGate_BranchOnlyPathGated pins AC1 (reconciler path): a
// branch-only integrate — no hydra/wt-<key> worktree exists, only the
// recorded branch — is still gated by the same scratch-worktree gate, and
// failure wraps ErrQuarantineSuiteGateBlocked with the checkout untouched.
// This is also the singleton close fallback path (Integrate delegates to
// IntegrateBranch).
func TestSuiteGate_BranchOnlyPathGated(t *testing.T) {
	suiteGateMoveRequireImpl(t)
	dir := newGateFixtureRepo(t)
	headBefore, _ := gitOut(dir, "rev-parse", "HEAD")

	// Create the branch alone — no worktree, exactly what the quarantine
	// reconciler sees after QuarantineBranchAndRemove removed the worktree.
	branch := "hydra/wt-branch-only-key"
	run := func(args ...string) {
		c := "cd " + dir + " && git " + strings.Join(args, " ")
		if out, err := execCommand(c); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "branch-work.txt"), []byte("w"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "branch-work")
	run("checkout", "-q", "main")

	m := NewWorktreeManager()
	gateFired := false
	m.SuiteGate = func(projectPath, worktreePath, key string) error {
		gateFired = true
		return errors.New("suite failed: branch-only tree broken")
	}

	if err := m.IntegrateBranch(dir, "branch-only-key", branch); err == nil {
		t.Fatal("branch-only integrate must be gated, got nil error")
	} else if !errors.Is(err, ErrQuarantineSuiteGateBlocked) {
		t.Fatalf("branch-only gate failure must wrap ErrQuarantineSuiteGateBlocked, got %v", err)
	}
	if !gateFired {
		t.Fatal("gate was not invoked on the branch-only path")
	}
	headAfter, _ := gitOut(dir, "rev-parse", "HEAD")
	if strings.TrimSpace(headAfter) != strings.TrimSpace(headBefore) {
		t.Fatalf("branch-only gate failure must not land work on the checkout: before=%s after=%s",
			strings.TrimSpace(headBefore), strings.TrimSpace(headAfter))
	}
}

// TestSuiteGate_ExactlyOneRunPerIntegrate pins the "not duplicated" clause:
// a clean chain's integrate (via IntegrateBranchAndRemove, which now only
// forwards to IntegrateBranch) runs the full-suite gate EXACTLY ONCE.
func TestSuiteGate_ExactlyOneRunPerIntegrate(t *testing.T) {
	suiteGateMoveRequireImpl(t)
	dir := newGateFixtureRepo(t)

	m := NewWorktreeManager()
	gateRuns := 0
	var gateSeenPath string
	m.SuiteGate = func(projectPath, worktreePath, key string) error {
		gateRuns++
		gateSeenPath = worktreePath
		return nil // suite green
	}
	wtPath, err := m.EnsureWorktree(dir, "one-run-key")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	commitWorkFile(t, wtPath, "one.txt")

	if err := m.IntegrateBranchAndRemove(dir, "one-run-key", m.branchName("one-run-key")); err != nil {
		t.Fatalf("clean integrate should succeed: %v", err)
	}
	if !strings.Contains(filepath.Base(gateSeenPath), "integrate-scratch-one-run-key") {
		t.Fatalf("gate must run on the integrate-scratch worktree, got path %q", gateSeenPath)
	}
	if gateRuns != 1 {
		t.Fatalf("full suite must run exactly ONCE per integrate, got %d runs", gateRuns)
	}
}

// TestSuiteGate_NilGateFailsOpen pins the fail-open clause: when the
// manager's SuiteGate and the package default are both nil, IntegrateBranch
// proceeds ungated and succeeds.
func TestSuiteGate_NilGateFailsOpen(t *testing.T) {
	suiteGateMoveRequireImpl(t)
	dir := newGateFixtureRepo(t)

	orig := defaultWorktreeSuiteGate
	defaultWorktreeSuiteGate = nil
	t.Cleanup(func() { defaultWorktreeSuiteGate = orig })
	_ = orig

	m := NewWorktreeManager()
	m.SuiteGate = nil
	wtPath, err := m.EnsureWorktree(dir, "nil-gate-key")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	commitWorkFile(t, wtPath, "ungated.txt")

	if err := m.IntegrateBranch(dir, "nil-gate-key", m.branchName("nil-gate-key")); err != nil {
		t.Fatalf("nil gate must fail open (integrate proceeds), got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ungated.txt")); err != nil {
		t.Fatalf("ungated integrate should land the work: %v", err)
	}
}

// TestSuiteGate_ReconcilerPathKeepsQuarantineAndEmitsTypedEvent pins AC2:
// when IntegrateBranch fails with ErrQuarantineSuiteGateBlocked, the
// quarantine reconciler distinguishes the typed error (no string sniffing),
// keeps the quarantine branch + entry, and emits exactly one
// quarantine_suite_gate_blocked event naming the chain.
func TestSuiteGate_ReconcilerPathKeepsQuarantineAndEmitsTypedEvent(t *testing.T) {
	suiteGateMoveRequireImpl(t)
	key := "test-chain-gate"
	dir, reader := setupReconcilerRepo(t, key, nil)

	sink := NewEventSink(t.TempDir())
	r := NewQuarantineReconciler(
		filepath.Join(t.TempDir(), "quarantined-chains.json"),
		NewWorktreeManager(), reader, sink,
		func() []types.Project { return []types.Project{{Name: "test", Path: dir}} })
	// Replace the reconciler's manager with one whose suite gate fails —
	// mimics the post-merge full-suite run finding broken work.
	m := NewWorktreeManager()
	m.SuiteGate = func(projectPath, worktreePath, chainKey string) error {
		return fmt.Errorf("suite failed: cross-bead breakage in %s", chainKey)
	}
	r.worktreeMgr = m

	r.Record(dir, "test", key, "hydra/quarantine/"+key, []types.BeadID{types.BeadID(key), "member-1"})
	r.Reconcile()

	if !branchExists(dir, "hydra/quarantine/"+key) {
		t.Fatal("gate-blocked chain: quarantine branch must survive")
	}
	if len(r.entries) != 1 {
		t.Fatalf("gate-blocked chain: entry must remain, got %d", len(r.entries))
	}
	blocked := readEventsOfType(t, sink, "quarantine_suite_gate_blocked")
	if len(blocked) != 1 {
		t.Fatalf("expected exactly one quarantine_suite_gate_blocked event, got %d", len(blocked))
	}
	if blocked[0].Bead != key {
		t.Fatalf("event should name the chain key, got %q", blocked[0].Bead)
	}
}
