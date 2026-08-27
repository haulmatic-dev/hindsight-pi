package core

import (
	"errors"
	"strings"
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

// Fail-closed: if GetBead cannot verify the bead's state, closeBead must abort
// rather than risk a stale/empty read. This was the fifth resurrection vector:
// a transient GetBead error bypassed the terminal guard and let a duplicate
// worker reopen an already-closed bead.
func TestClose_FailClosedOnGetBeadError(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "x/a.go", "package x\n")

	mock := newMockBeadsClient()
	mock.beads["testproject-geterr"] = types.Bead{ID: "testproject-geterr", Status: types.StatusOpen}
	mock.getErr = errors.New("br daemon unavailable")

	cfg := DefaultConfig()
	resolver := NewProjectResolver([]types.Project{
		{Name: "testproject", Path: repo, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(mock, nil, resolver, nil, cfg)

	worker := &types.Worker{BeadID: types.BeadID("testproject-geterr")}
	if err := closer.Close(worker, true, 0); err == nil || !strings.Contains(err.Error(), "cannot verify") {
		t.Fatalf("closeBead must fail closed on GetBead error, got: %v", err)
	}

	mock.getErr = nil
	got := mock.beads["testproject-geterr"]
	if got.Status != types.StatusOpen {
		t.Fatalf("bead status must not change when verification fails, got: %+v", got)
	}
	if len(mock.closeCalls) != 0 {
		t.Fatalf("CloseBead must not be called when verification fails, got %+v", mock.closeCalls)
	}
}

// Fail-closed: a missing/stale open read from GetBead is also an unverifiable
// state; the guard must not silently treat it as "okay to close".
func TestClose_FailClosedOnMissingBead(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "x/a.go", "package x\n")

	mock := newMockBeadsClient()
	// bead is intentionally not registered

	cfg := DefaultConfig()
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: repo, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(mock, nil, resolver, nil, cfg)

	worker := &types.Worker{BeadID: types.BeadID("testproject-missing")}
	if err := closer.Close(worker, true, 0); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("closeBead must fail closed when bead is missing, got: %v", err)
	}
	if len(mock.closeCalls) != 0 {
		t.Fatalf("CloseBead must not be called for missing bead, got %+v", mock.closeCalls)
	}
}

// Fail-closed: rejectClose must not reopen a bead when GetBead fails, because
// a close we cannot verify is a close we must not perform.
func TestRejectClose_FailClosedOnGetBeadError(t *testing.T) {
	repo := initGitRepo(t)

	mock := newMockBeadsClient()
	mock.beads["testproject-reject"] = types.Bead{ID: "testproject-reject", Status: types.StatusOpen}
	mock.getErr = errors.New("br daemon unavailable")

	cfg := DefaultConfig()
	resolver := NewProjectResolver([]types.Project{
		{Name: "testproject", Path: repo, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(mock, nil, resolver, nil, cfg)

	bead := &types.Bead{ID: "testproject-reject", Project: "testproject"}
	if err := closer.rejectClose(bead, repo, "", "empty-diff"); err == nil || !strings.Contains(err.Error(), "cannot verify") {
		t.Fatalf("rejectClose must fail closed on GetBead error, got: %v", err)
	}

	mock.getErr = nil
	got := mock.beads["testproject-reject"]
	if got.Status != types.StatusOpen {
		t.Fatalf("bead must stay open when verification fails, got: %+v", got)
	}
	if strings.Join(got.Labels, " ") != "" {
		t.Fatalf("rejectClose must not label a bead it could not verify, got labels: %v", got.Labels)
	}
}

// Fail-closed: verifyBeforeClose must abort when GetBead errors instead of
// running diff/scope gates on a potentially stale snapshot.
func TestVerifyBeforeClose_FailClosedOnGetBeadError(t *testing.T) {
	repo := initGitRepo(t)

	mock := newMockBeadsClient()
	mock.beads["testproject-verify"] = types.Bead{ID: "testproject-verify", Status: types.StatusOpen}
	mock.getErr = errors.New("br daemon unavailable")

	cfg := DefaultConfig()
	cfg.Validation.Enabled = true
	cfg.Validation.RequireDiffOnClose = true
	cfg.Validation.EnforceTargetScope = "block"
	resolver := NewProjectResolver([]types.Project{
		{Name: "testproject", Path: repo, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(mock, nil, resolver, nil, cfg)

	bead := &types.Bead{ID: "testproject-verify", Project: "testproject"}
	if err := closer.verifyBeforeClose(bead, repo, ""); err == nil || !strings.Contains(err.Error(), "cannot verify") {
		t.Fatalf("verifyBeforeClose must fail closed on GetBead error, got: %v", err)
	}

	mock.getErr = nil
	got := mock.beads["testproject-verify"]
	if got.Status != types.StatusOpen {
		t.Fatalf("bead must stay open when verification fails, got: %+v", got)
	}
}
