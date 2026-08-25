package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// The 2026-08-24/25 resurrection bug: a second close attempt after a
// successful close+integration found an empty diff, and rejectClose REOPENED
// the already-closed bead (bz5, aga, i1g, 5r0 all accepted-then-reopened).
// rejectClose must be a no-op on terminal beads.
func TestRejectClose_TerminalBeadIsNoOp(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "x/a.go", "package x\n")

	mock := newMockBeadsClient()
	mock.beads["term-bead"] = types.Bead{ID: "term-bead", Status: types.StatusClosed}

	cfg := DefaultConfig()
	cfg.Validation.Enabled = true
	cfg.Validation.RequireDiffOnClose = true
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: repo, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(mock, nil, resolver, nil, cfg)

	bead := &types.Bead{ID: "term-bead", Project: "test-project"}
	if err := closer.rejectClose(bead, repo, "", "empty-diff"); err != nil {
		t.Fatalf("rejectClose on a terminal bead must return nil (no-op), got: %v", err)
	}
	got, _ := mock.GetBead(repo, "", "term-bead")
	if got == nil || got.Status != types.StatusClosed {
		t.Fatalf("terminal bead must stay closed, got: %+v", got)
	}
}

// Same guard at the verify entrypoint: a bead that is already terminal must
// not even run the gates/diff checks.
func TestVerifyBeforeClose_TerminalBeadSkips(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "x/a.go", "package x\n")

	mock := newMockBeadsClient()
	mock.beads["term-bead-2"] = types.Bead{ID: "term-bead-2", Status: types.StatusClosed}

	cfg := DefaultConfig()
	cfg.Validation.Enabled = true
	cfg.Validation.RequireDiffOnClose = true
	cfg.Validation.EnforceTargetScope = "block"
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: repo, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(mock, nil, resolver, nil, cfg)

	// Bead targets a file that does NOT exist — would normally reject
	// empty-diff; the terminal guard must short-circuit first.
	bead := &types.Bead{
		ID: "term-bead-2", Project: "test-project",
		Metadata: map[string]string{"files": "x/missing.go"},
	}
	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("verifyBeforeClose on a terminal bead must return nil, got: %v", err)
	}
	got, _ := mock.GetBead(repo, "", "term-bead-2")
	if got == nil || got.Status != types.StatusClosed {
		t.Fatalf("terminal bead must stay closed, got: %+v", got)
	}
}
