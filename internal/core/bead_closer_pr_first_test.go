package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// installPRFirstFake stubs the exec hooks used by PublishBranchViaPR /
// publishSingletonViaPR so no real git push or gh call happens.
func installPRFirstFake(t *testing.T, pushRef *string, createBase, createHead *string) func() {
	t.Helper()
	origPush := prMergeBranchPush
	origCreate := autoPRGhCreate
	origMerge := autoPRGhMerge
	prMergeBranchPush = func(repoPath, localRef, ref, token string) error {
		if pushRef != nil {
			*pushRef = localRef
		}
		return nil
	}
	autoPRGhCreate = func(repoPath, base, head, title, body, token string) (string, error) {
		if createBase != nil {
			*createBase = base
		}
		if createHead != nil {
			*createHead = head
		}
		return "https://github.com/x/y/pull/1", nil
	}
	autoPRGhMerge = func(repoPath, head, token string) error { return nil }
	return func() {
		prMergeBranchPush = origPush
		autoPRGhCreate = origCreate
		autoPRGhMerge = origMerge
	}
}

// TestPublishBranchViaPR_LandsViaPR proves the branch-PR publisher pushes
// the named local branch (not HEAD), creates the PR against the given base,
// merges it, and only then reports success.
func TestPublishBranchViaPR_LandsViaPR(t *testing.T) {
	var pushedRef, createBase, createHead string
	restore := installPRFirstFake(t, &pushedRef, &createBase, &createHead)
	defer restore()

	url, err := PublishBranchViaPR("/tmp/x", "development", "hydra/wt-abc", "dogfood/abc", "T", "tok")
	if err != nil {
		t.Fatalf("PublishBranchViaPR: %v", err)
	}
	if url != "https://github.com/x/y/pull/1" {
		t.Fatalf("url = %q", url)
	}
	if pushedRef != "hydra/wt-abc" {
		t.Fatalf("pushed localRef = %q, want hydra/wt-abc", pushedRef)
	}
	if createBase != "development" {
		t.Fatalf("PR base = %q, want development", createBase)
	}
	if createHead != "dogfood/abc" {
		t.Fatalf("PR head = %q, want dogfood/abc", createHead)
	}
}

// TestPublishBranchViaPR_FailsClosedOnCreateError proves a failed PR create
// never reports the integration as landed.
func TestPublishBranchViaPR_FailsClosedOnCreateError(t *testing.T) {
	origCreate := autoPRGhCreate
	autoPRGhCreate = func(repoPath, base, head, title, body, token string) (string, error) {
		return "", execErr("gh pr create failed")
	}
	defer func() { autoPRGhCreate = origCreate }()

	if _, err := PublishBranchViaPR("/tmp/x", "development", "hydra/wt-abc", "dogfood/abc", "T", ""); err == nil {
		t.Fatal("expected error from failed PR create")
	}
}

func execErr(s string) error { return &exec.ExitError{} }

// TestBeadCloser_SingletonWithRemoteUsesPRFirst proves the FR-010 contract:
// when the project has a remote, Close pushes the worktree branch and lands
// it via PR WITHOUT merging it into the local working branch.
func TestBeadCloser_SingletonWithRemoteUsesPRFirst(t *testing.T) {
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	// Configure a remote (bare repo) so hasGitRemote is true.
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare", "--initial-branch=main")
	runGit(t, projectPath, "remote", "add", "origin", bare)
	// Push the base so the resolver finds an origin default (deterministic
	// resolution, telematics 2026-09-29); a remote without any branch cannot
	// yield a PR base.
	runGit(t, projectPath, "push", "-u", "origin", "main")

	m := NewWorktreeManager()
	beadID := types.BeadID("singleton-pr")
	key := beadID.String()

	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	writeFile(t, wt, "feature.txt", "feature work\n")

	mockBeads := newMockBeadsClient()
	mockBeads.addBead(types.Bead{
		ID:      beadID,
		Title:   "singleton bead",
		Project: "singleton",
		Status:  types.StatusInProgress,
	})

	// tipMergedProbe must say "not already landed" so the PR path runs.
	origProbe := tipMergedProbe
	tipMergedProbe = func(string, string) bool { return false }
	defer func() { tipMergedProbe = origProbe }()

	var pushedRef, createBase, createHead string
	restore := installPRFirstFake(t, &pushedRef, &createBase, &createHead)
	defer restore()

	closer := newTestBeadCloser(t, "singleton", projectPath, m,
		func(b types.Bead) string { return b.ID.String() },
		func(types.BeadID) bool { return false },
		mockBeads,
	)

	if err := closer.Close(&types.Worker{BeadID: beadID}, true, 0); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if pushedRef != "hydra/wt-"+key {
		t.Fatalf("pushed localRef = %q, want hydra/wt-%s", pushedRef, key)
	}
	if createHead != "dogfood/"+key {
		t.Fatalf("PR head = %q, want dogfood/%s", createHead, key)
	}
	if createBase == "" {
		t.Fatal("PR create never called")
	}

	// The local working branch must NOT contain the feature — no local merge.
	if _, err := os.Stat(filepath.Join(projectPath, "feature.txt")); err == nil {
		t.Fatal("feature.txt leaked into local working branch; local merge should not run under PR-first contract")
	}

	// Worktree branch is torn down after the PR lands.
	if branchExists(projectPath, "hydra/wt-"+key) {
		t.Fatal("expected singleton worktree branch to be removed after PR-first close")
	}
}

// TestBeadCloser_SingletonNoRemoteKeepsLocalMerge proves the no-remote
// fallback still merges locally (existing contract preserved).
func TestBeadCloser_SingletonNoRemoteKeepsLocalMerge(t *testing.T) {
	projectPath := initGitRepoWithCommit(t) // no remote
	t.Setenv("HYDRA_HOME", t.TempDir())

	m := NewWorktreeManager()
	beadID := types.BeadID("singleton-nr")
	key := beadID.String()

	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	writeFile(t, wt, "feature.txt", "feature work\n")

	mockBeads := newMockBeadsClient()
	mockBeads.addBead(types.Bead{
		ID:      beadID,
		Title:   "singleton bead",
		Project: "singleton",
		Status:  types.StatusInProgress,
	})

	closer := newTestBeadCloser(t, "singleton", projectPath, m,
		func(b types.Bead) string { return b.ID.String() },
		func(types.BeadID) bool { return false },
		mockBeads,
	)

	if err := closer.Close(&types.Worker{BeadID: beadID}, true, 0); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(projectPath, "feature.txt"))
	if err != nil {
		t.Fatalf("expected local merge fallback to land feature.txt: %v", err)
	}
	if string(got) != "feature work\n" {
		t.Fatalf("feature.txt = %q", got)
	}
}
