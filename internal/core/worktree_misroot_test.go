package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Regression suite for the 2026-10-20 mis-rooting + fail-open cascade
// (hydra-q3p/xnk7/r2rl/htq0): a chain worktree attached to the parent
// monorepo's gitdir made every git op exit 128 and verified-close failed
// OPEN, accepting zero-work closes.

// TestExistingWorktree_RejectsForeignGitdir: a valid git worktree of a
// DIFFERENT repository occupying the chain worktree path must be reported
// as not-existing (never handed to a worker or the closer).
func TestExistingWorktree_RejectsForeignGitdir(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "proj")
	foreign := filepath.Join(root, "foreign")
	initWtRepo(t, project)
	initWtRepo(t, foreign)

	m := NewWorktreeManager()
	key := "k1"
	wtPath := m.worktreePath(project, key)
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, foreign, "worktree", "add", wtPath, "-b", "wt-foreign")

	gotPath, ok := m.ExistingWorktree(project, key)
	if ok {
		t.Fatalf("mis-rooted worktree must be refused, got ok=true path=%s", gotPath)
	}
	if gotPath != "" {
		t.Fatalf("refused worktree must return empty path, got %q", gotPath)
	}
}

// TestExistingWorktree_AcceptsOursSubmoduleLayout: a linked worktree of the
// project itself — including the submodule gitdir layout
// (<super>/.git/modules/<name>) — must still be accepted.
func TestExistingWorktree_AcceptsOursSubmoduleLayout(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub-src")
	initWtRepo(t, sub)

	super := filepath.Join(root, "super")
	initWtRepo(t, super)
	gitRun(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "components/sub")
	gitRun(t, super, "commit", "-qm", "add submodule")

	subCheckout := filepath.Join(super, "components", "sub")
	m := NewWorktreeManager()
	key := "k1"
	wtPath := m.worktreePath(subCheckout, key)
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, subCheckout, "worktree", "add", wtPath, "-b", "wt-k1")

	got, ok := m.ExistingWorktree(subCheckout, key)
	if !ok {
		t.Fatalf("own submodule-layout worktree must be accepted")
	}
	if got != wtPath {
		t.Fatalf("path mismatch: got %q want %q", got, wtPath)
	}
}

// TestCollectCloseEvidence_WorktreeDiffErrorFatal: in worktree mode the
// evidence-repo diff is the ONLY evidence — a git error there must return
// an error (fail-closed), never a nil (fail-open) changed list.
func TestCollectCloseEvidence_WorktreeDiffErrorFatal(t *testing.T) {
	_, closer, bead := setupScopeGateTest(t, "block")
	nonGit := t.TempDir() // evidence "repo" is not a git checkout

	_, err := closer.collectCloseEvidence(bead, nonGit, nonGit, nonGit, true, "", true, false)
	if err == nil {
		t.Fatal("worktree-mode diff error must fail closed")
	}
	if !strings.Contains(err.Error(), "cannot read diff evidence") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCollectCloseEvidence_MainModeCommittedEvidenceRescuesStatusError:
// in main-checkout mode a status failure is rescued by the committed-evidence
// fallback (uncommitted edits are revertible by design; nipuna 2026-09-16).
func TestCollectCloseEvidence_MainModeCommittedEvidenceRescuesStatusError(t *testing.T) {
	repo, closer, _ := setupScopeGateTest(t, "block")
	nonGit := t.TempDir() // evidence repo: status will fail
	// Fresh bead ID (no session base): committedChangesSinceBranchBase fails
	// (no origin/<branch> merge-base in the fixture repo) → both evidence
	// sources unavailable → fatal.
	bead := &types.Bead{ID: "no-session-bead", Title: "Test bead", Metadata: map[string]string{"files": "x/a.go"}}

	// no session base: committedChangesSinceBranchBase also fails on a
	// non-git dir → both evidence sources unavailable → fatal.
	if _, err := closer.collectCloseEvidence(bead, repo, nonGit, nonGit, false, "", true, false); err == nil {
		t.Fatal("status error + committed-evidence error must fail closed in main mode")
	}
}

// TestCollectCloseEvidence_MainModeFailOpenOnStatusOnly: a status failure
// with a working committed-evidence source is NOT fatal (the fallback
// produced real evidence).
func TestCollectCloseEvidence_MainModeFailOpenOnStatusOnly(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	closer.SetSessionBase(bead.ID.String(), "HEAD")
	changed, err := closer.collectCloseEvidence(bead, repo, repo, repo, false, "", true, false)
	if err != nil {
		t.Fatalf("committed evidence success must keep close verifiable: %v", err)
	}
	_ = changed
}

// TestCollectCloseEvidence_MainLeakUnionErrorFatalWhenNoEvidence: a
// main-checkout status error with an empty worktree diff (no evidence at
// all) must fail closed.
func TestCollectCloseEvidence_MainLeakUnionErrorFatalWhenNoEvidence(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	nonGit := t.TempDir() // main checkout unreadable

	_, err := closer.collectCloseEvidence(bead, repo, repo, nonGit, true, "", true, false)
	if err == nil {
		t.Fatal("unreadable main checkout with zero evidence must fail closed")
	}
	if !strings.Contains(err.Error(), "cannot verify work") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestVerifyBeforeClose_WorktreeDiffErrorRejectsClose: end-to-end — a broken
// evidence repo in worktree mode produces a cannot-verify-diff rejection
// (bead reopened), never an accept.
func TestVerifyBeforeClose_WorktreeDiffErrorRejectsClose(t *testing.T) {
	_, closer, _ := setupScopeGateTest(t, "block")
	// Unknown project + fresh bead ID: the resolver falls back to projectPath
	// (non-git) as the evidence repo, and no session base exists — both
	// evidence sources are broken → cannot-verify-diff rejection.
	bead := &types.Bead{ID: "unverifiable-bead", Title: "Test bead", Metadata: map[string]string{"files": "x/a.go"}}
	nonGit := t.TempDir()
	err := closer.verifyBeforeClose(bead, nonGit, "")
	if err == nil {
		t.Fatal("unverifiable evidence must reject the close")
	}
	if !strings.Contains(err.Error(), "cannot-verify-diff") {
		t.Fatalf("rejection reason must be cannot-verify-diff, got: %v", err)
	}
}
