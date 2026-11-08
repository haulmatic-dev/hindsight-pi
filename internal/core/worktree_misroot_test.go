package core

import (
	"fmt"
	"os"
	"os/exec"
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
	// AND the evidence repo is a NON-git dir → git reports "not a git
	// repository" → no evidence surface by design → legacy fail-open
	// (worker_completion fixture contract; git cannot distinguish an
	// unreadable .git from a non-repo either). Real-repo git faults are
	// pinned by the evidenceFatalErr classifier table below.
	bead := &types.Bead{ID: "no-session-bead", Title: "Test bead", Metadata: map[string]string{"files": "x/a.go"}}
	if _, err := closer.collectCloseEvidence(bead, repo, nonGit, nonGit, false, "", true, false); err != nil {
		t.Fatalf("non-repo evidence surface must stay legacy fail-open, got: %v", err)
	}
}

// TestEvidenceFatalErr_Classifier: FAIL-CLOSED is scoped to real-repo git
// faults (dubious ownership, permission denied, corruption). A non-repo path
// or a missing directory has no evidence surface — legacy fail-open.
func TestEvidenceFatalErr_Classifier(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		fatal bool
	}{
		{"nil", nil, false},
		{"not a git repository", &exec.ExitError{Stderr: []byte("fatal: not a git repository (or any of the parent directories): .git")}, false},
		{"dubious ownership", &exec.ExitError{Stderr: []byte("fatal: detected dubious ownership in repository at '/x'")}, true},
		{"permission denied", &exec.ExitError{Stderr: []byte("fatal: Unable to create '/x/.git/index.lock': Permission denied")}, true},
		{"missing dir", fmt.Errorf("chdir /gone: no such file or directory"), false},
		{"fork/exec env fault", fmt.Errorf("fork/exec /bin/git: resource temporarily unavailable"), true},
	}
	for _, tc := range cases {
		if got := evidenceFatalErr(tc.err); got != tc.fatal {
			t.Errorf("%s: evidenceFatalErr=%v want %v", tc.name, got, tc.fatal)
		}
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

// TestCollectCloseEvidence_MainLeakUnionErrorLegacyForNonRepo: a
// main-checkout path that is not a git repo is legacy fail-open even with an
// empty worktree diff (worker_completion fixture contract).
func TestCollectCloseEvidence_MainLeakUnionErrorLegacyForNonRepo(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	nonGit := t.TempDir() // main checkout: no evidence surface

	if _, err := closer.collectCloseEvidence(bead, repo, repo, nonGit, true, "", true, false); err != nil {
		t.Fatalf("non-repo main checkout must stay fail-open, got: %v", err)
	}
}

// TestVerifyBeforeClose_WorktreeDiffErrorRejectsClose: end-to-end — an
// evidence failure in WORKTREE mode fails closed (cannot-read-diff error);
// worktree mode is blanket fail-closed because ExistingWorktree already
// guarantees a real repo identity.
func TestVerifyBeforeClose_WorktreeDiffErrorRejectsClose(t *testing.T) {
	_, closer, _ := setupScopeGateTest(t, "block")
	_, err := closer.collectCloseEvidence(&types.Bead{ID: "unverifiable-bead", Title: "Test bead", Metadata: map[string]string{"files": "x/a.go"}}, "", "/definitely/not/a/repo", "", true, "", true, false)
	if err == nil {
		t.Fatal("worktree-mode evidence failure must fail closed")
	}
	if !strings.Contains(err.Error(), "cannot read diff evidence") {
		t.Fatalf("unexpected error: %v", err)
	}
}
