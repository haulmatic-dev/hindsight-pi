package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// 2026-09-16 close-reopen loop (nipuna webapp campaign): accepted closes
// failed at the integrate stage, failIntegration reopened the beads, and the
// plain reopen let them respawn into the same failing integrate forever.
// These tests pin the three fixes: the union merge driver for CHANGELOG.md,
// the wrong-branch worktree repair, and the integration-exhausted cap.

func TestEnsureChangelogUnionDriver_Idempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)

	ensureChangelogUnionDriver(proj)
	ensureChangelogUnionDriver(proj)

	data, err := os.ReadFile(filepath.Join(proj, ".git", "info", "attributes"))
	if err != nil {
		t.Fatalf("attributes not written: %v", err)
	}
	if n := strings.Count(string(data), changelogUnionEntry); n != 1 {
		t.Fatalf("expected exactly one %q entry, got %d in:\n%s", changelogUnionEntry, n, data)
	}
}

func TestEnsureWorktree_RepairsWrongBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "webapp-9ah")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// Simulate the 2026-09-16 webapp-9ah storm: the worktree ends up sitting
	// on the campaign branch instead of its hydra/wt-* branch, with no
	// unintegrated commits.
	runGitCmd(t, wt, "checkout", "-b", "technical/flow-runner-platform-20260915")

	wt2, err := m.EnsureWorktree(proj, "webapp-9ah")
	if err != nil {
		t.Fatalf("EnsureWorktree repair: %v", err)
	}
	if wt2 != wt {
		t.Fatalf("repair must reuse the same path: %s vs %s", wt2, wt)
	}
	out, err := gitOut(wt, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(out) != m.branchName("webapp-9ah") {
		t.Fatalf("expected repair back to %s, got %q (err %v)", m.branchName("webapp-9ah"), out, err)
	}
}

func TestEnsureWorktree_RefusesWrongBranchWithUnintegratedCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(proj, "webapp-9ah")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// Worker committed directly onto the campaign branch inside the worktree:
	// the commits are real output and must not be destroyed by a repair.
	runGitCmd(t, wt, "checkout", "-b", "technical/flow-runner-platform-20260915")
	if err := os.WriteFile(filepath.Join(wt, "work.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, wt, "add", "-A")
	runGitCmd(t, wt, "commit", "-m", "worker output on wrong branch")

	if _, err := m.EnsureWorktree(proj, "webapp-9ah"); err == nil || !strings.Contains(err.Error(), "wrong branch") {
		t.Fatalf("expected wrong-branch refusal preserving unintegrated work, got: %v", err)
	}
}

func TestFailIntegration_EscalatesToExhaustedAtCap(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "x/a.go", "package x\n")

	mock := newMockBeadsClient()
	mock.beads["webapp-9ah"] = types.Bead{ID: "webapp-9ah", Status: types.StatusInProgress}

	resolver := NewProjectResolver([]types.Project{
		{Name: "webapp", Path: repo, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(mock, nil, resolver, nil, DefaultConfig())

	bead := &types.Bead{ID: "webapp-9ah", Project: "webapp"}
	cause := errors.New("merge conflict in CHANGELOG.md")

	for i := 1; i <= maxConsecutiveIntegrationFailures; i++ {
		if err := closer.failIntegration(bead, repo, "webapp", "integrate", cause); err == nil {
			t.Fatalf("failIntegration must always return a non-nil error (attempt %d)", i)
		}
		got, _ := mock.GetBead(repo, "webapp", "webapp-9ah")
		if got == nil || got.Status != types.StatusOpen {
			t.Fatalf("attempt %d: bead must be reopened (open), got %+v", i, got)
		}
		exhausted := hasBeadLabel(got.Labels, integrationExhaustedLabel)
		if i < maxConsecutiveIntegrationFailures && exhausted {
			t.Fatalf("attempt %d: must not escalate before the cap", i)
		}
		if i == maxConsecutiveIntegrationFailures && !exhausted {
			t.Fatalf("attempt %d: must label %q at the cap, labels=%v", i, integrationExhaustedLabel, got.Labels)
		}
		if !hasBeadLabel(got.Labels, "integration-failed") {
			t.Fatalf("attempt %d: integration-failed label missing, labels=%v", i, got.Labels)
		}
	}
}

func TestIntegrationSuccess_ResetsFailureCounter(t *testing.T) {
	mock := newMockBeadsClient()
	mock.beads["webapp-9ah"] = types.Bead{ID: "webapp-9ah", Status: types.StatusInProgress}
	closer := NewBeadCloser(mock, nil, nil, nil, DefaultConfig())
	bead := &types.Bead{ID: "webapp-9ah", Project: "webapp"}

	// One failure below the cap, then a simulated success: the counter must
	// reset so a later isolated failure does not trip the cap early.
	if err := closer.failIntegration(bead, "/nonexistent", "webapp", "push", errors.New("boom")); err == nil {
		t.Fatal("failIntegration must return non-nil")
	}
	closer.resetIntegrationFailures("webapp-9ah")

	for i := 1; i < maxConsecutiveIntegrationFailures; i++ {
		_ = closer.failIntegration(bead, "/nonexistent", "webapp", "push", errors.New("boom"))
	}
	got, _ := mock.GetBead("/nonexistent", "webapp", "webapp-9ah")
	if hasBeadLabel(got.Labels, integrationExhaustedLabel) {
		t.Fatalf("counter reset broken: exhausted after only %d fresh failures", maxConsecutiveIntegrationFailures-1)
	}
}
