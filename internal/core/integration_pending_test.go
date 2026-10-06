package core

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// 2026-10-02 srv-jobfile-65l+5my: beads whose work was COMPLETE re-entered
// the ready queue on every integrate 403 and were fully re-run by a fresh
// LLM worker (~11 spawns/45min per bead) because failIntegration reopened
// them to open. The fix: park in place with integration-pending; the
// mechanical retest stage owns the retry. These tests pin that every
// respawn path skips pending beads.

func newPendingFixture(t *testing.T) (*mockBeadsClient, *BeadCloser, *types.Bead, string) {
	t.Helper()
	mock := newMockBeadsClient()
	mock.beads["webapp-9ah"] = types.Bead{ID: "webapp-9ah", Status: types.StatusInProgress}
	closer := NewBeadCloser(mock, nil, nil, nil, DefaultConfig())
	bead := &types.Bead{ID: "webapp-9ah", Project: "webapp"}
	return mock, closer, bead, "webapp"
}

func TestFailIntegration_ParksPendingNotReopen(t *testing.T) {
	oldBase := integrationBackoffBase
	integrationBackoffBase = 0
	defer func() { integrationBackoffBase = oldBase }()

	mock, closer, bead, repo := newPendingFixture(t)
	if err := closer.failIntegration(bead, "/nonexistent", repo, "integrate", errors.New("gh pr merge: exit status 1: HTTP 403")); err == nil {
		t.Fatal("failIntegration must return non-nil")
	}
	got, _ := mock.GetBead("/nonexistent", repo, "webapp-9ah")
	if got.Status != types.StatusInProgress {
		t.Fatalf("pending park must NOT reopen: status = %s, want in_progress", got.Status)
	}
	if !hasBeadLabel(got.Labels, integrationPendingLabel) {
		t.Fatalf("integration-pending label missing: %v", got.Labels)
	}
	if !hasBeadLabel(got.Labels, retestLabelFailed) {
		t.Fatalf("integration-failed label missing (retest selector needs it): %v", got.Labels)
	}
}

func TestFailIntegration_PendingNotCountedAsOrphanByDigest(t *testing.T) {
	oldBase := integrationBackoffBase
	integrationBackoffBase = 0
	defer func() { integrationBackoffBase = oldBase }()

	mock, closer, bead, repo := newPendingFixture(t)
	_ = closer.failIntegration(bead, "/nonexistent", repo, "integrate", errors.New("boom"))

	fresh, _ := mock.GetBead(repo, repo, "webapp-9ah")
	if hasBeadLabel(fresh.Labels, integrationPendingLabel) && hasBeadLabel(fresh.Labels, integrationExhaustedLabel) {
		t.Fatal("single failure must not trip the exhaustion cap")
	}
}

func TestRetestSelector_SelectsPendingBead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "README.md", "seed\n")
	runGitCmd(t, repo, "branch", "hydra/wt-webapp-9ah")

	s := NewIntegrationRetestStage(IntegrationRetestConfig{Enabled: true}, DefaultConfig(), nil, nil, nil, t.TempDir())
	bead := &types.Bead{
		ID:     "webapp-9ah",
		Status: types.StatusInProgress,
		Labels: []string{"integration-failed", integrationPendingLabel},
	}
	if !s.selectBead(repo, bead, nil) {
		t.Fatal("retest stage must select a pending bead with a live hydra/wt-* branch")
	}
}
