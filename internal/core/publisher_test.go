package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestBranchAheadOfRemote covers the three states: never-pushed (ahead),
// up-to-date (not ahead), and ahead-by-one (ahead).
func TestBranchAheadOfRemote(t *testing.T) {
	base := t.TempDir()
	bare := filepath.Join(base, "remote.git")
	repo := filepath.Join(base, "work")
	gitRun(t, base, "init", "--bare", bare)
	gitRun(t, base, "clone", bare, repo)
	gitRun(t, repo, "config", "user.email", "t@e.co")
	gitRun(t, repo, "config", "user.name", "T")
	gitRun(t, repo, "commit", "--allow-empty", "-m", "c1")

	branch := "master"
	if out, _ := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output(); len(out) > 0 {
		branch = string(out[:len(out)-1])
	}

	// Never pushed → ahead.
	if !branchAheadOfRemote(repo, branch) {
		t.Error("never-pushed branch must be reported ahead")
	}
	// Push, then up-to-date → not ahead.
	gitRun(t, repo, "push", "origin", branch)
	if branchAheadOfRemote(repo, branch) {
		t.Error("just-pushed branch must not be reported ahead")
	}
	// New commit → ahead again.
	gitRun(t, repo, "commit", "--allow-empty", "-m", "c2")
	if !branchAheadOfRemote(repo, branch) {
		t.Error("branch with an unpushed commit must be reported ahead")
	}
}

func TestPublishRepo_NilManager(t *testing.T) {
	if ok, reason := PublishRepo(nil, "/x", "tok", false, nil, nil); ok || reason != "no worktree manager" {
		t.Errorf("nil manager: got (%v,%q)", ok, reason)
	}
}

// TestPublishRepo_PushesAheadBranch: with a real bare remote and a commit not
// yet pushed, PublishRepo pushes the working branch (token empty → plain
// origin push fallback) and the commit lands on the remote.
func TestPublishRepo_PushesAheadBranch(t *testing.T) {
	base := t.TempDir()
	bare := filepath.Join(base, "remote.git")
	repo := filepath.Join(base, "work")
	gitRun(t, base, "init", "--bare", bare)
	gitRun(t, base, "clone", bare, repo)
	gitRun(t, repo, "config", "user.email", "t@e.co")
	gitRun(t, repo, "config", "user.name", "T")
	gitRun(t, repo, "commit", "--allow-empty", "-m", "c1")

	pushed, reason := PublishRepo(NewWorktreeManager(), repo, "", false, nil, func(string) bool { return true })
	if !pushed {
		t.Fatalf("expected push, got (false, %q)", reason)
	}
	// Commit must now exist on the bare remote.
	out, err := exec.Command("git", "-C", bare, "log", "--oneline").CombinedOutput()
	if err != nil || len(out) == 0 {
		t.Fatalf("remote has no commits after publish: %v\n%s", err, out)
	}
}

type autoPRFakeBeadsClient struct {
	labels       map[string][]string
	descriptions map[string]string
}

func (m *autoPRFakeBeadsClient) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}
func (m *autoPRFakeBeadsClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	return nil, nil
}
func (m *autoPRFakeBeadsClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	return nil
}
func (m *autoPRFakeBeadsClient) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}
func (m *autoPRFakeBeadsClient) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	return nil
}
func (m *autoPRFakeBeadsClient) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	return "", nil
}
func (m *autoPRFakeBeadsClient) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	if m.labels == nil {
		m.labels = make(map[string][]string)
	}
	m.labels[string(beadID)] = labels
	return nil
}

func (m *autoPRFakeBeadsClient) RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	if m.labels == nil {
		m.labels = make(map[string][]string)
	}
	m.labels[string(beadID)] = labels
	return nil
}
func (m *autoPRFakeBeadsClient) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	if m.descriptions == nil {
		m.descriptions = make(map[string]string)
	}
	m.descriptions[string(beadID)] = description
	return nil
}

func (m *autoPRFakeBeadsClient) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	return nil
}

func (m *autoPRFakeBeadsClient) AddDependency(projectPath, repoName string, beadID, dependsOn types.BeadID) error {
	return nil
}

type autoPRFakeExec struct {
	pushes      []autoPRPushArgs
	lsRemote    []autoPRLsArgs
	ghCreate    []autoPRGhArgs
	prPushes    []autoPRPushArgs
	ghMerge     []prMergeGhArgs
	ghListOpen  []string
	ghURL       string
	ghErr       error
	mergeErr    error
	lsFound     bool
	listOpen    bool
	listOpenErr error
}

type prMergeGhArgs struct {
	repoPath, head, token string
}

type autoPRPushArgs struct {
	repoPath, ref, token string
}

type autoPRLsArgs struct {
	repoPath, remote, ref string
}

type autoPRGhArgs struct {
	repoPath, base, head, title, body, token string
}

func installAutoPRFake(f *autoPRFakeExec) func() {
	origPush := autoPRGitPush
	origLs := autoPRLsRemote
	origGh := autoPRGhCreate
	origPrPush := prMergeGitPush
	origMerge := autoPRGhMerge
	origList := autoPRGhListOpen
	autoPRGitPush = func(repoPath, ref, token string) error {
		f.pushes = append(f.pushes, autoPRPushArgs{repoPath, ref, token})
		return nil
	}
	autoPRLsRemote = func(repoPath, remote, ref string) (string, error) {
		f.lsRemote = append(f.lsRemote, autoPRLsArgs{repoPath, remote, ref})
		if f.lsFound {
			return "abc\t" + ref + "\n", nil
		}
		return "", nil
	}
	autoPRGhCreate = func(repoPath, base, head, title, body, token string) (string, error) {
		f.ghCreate = append(f.ghCreate, autoPRGhArgs{repoPath, base, head, title, body, token})
		if f.ghErr != nil {
			return "", f.ghErr
		}
		return f.ghURL, nil
	}
	prMergeGitPush = func(repoPath, ref, token string) error {
		f.prPushes = append(f.prPushes, autoPRPushArgs{repoPath, ref, token})
		return nil
	}
	autoPRGhMerge = func(repoPath, head, token string) error {
		f.ghMerge = append(f.ghMerge, prMergeGhArgs{repoPath, head, token})
		return f.mergeErr
	}
	autoPRGhListOpen = func(repoPath, head, token string) (bool, error) {
		f.ghListOpen = append(f.ghListOpen, head)
		return f.listOpen, f.listOpenErr
	}
	return func() {
		autoPRGitPush = origPush
		autoPRLsRemote = origLs
		autoPRGhCreate = origGh
		prMergeGitPush = origPrPush
		autoPRGhMerge = origMerge
		autoPRGhListOpen = origList
	}
}

// installPRMergeFake overrides the PR-merge exec hooks (force push + gh pr
// merge). Returning nil from the merge hook simulates a verified MERGED PR.
func installPRMergeFake(f *autoPRFakeExec) func() {
	origPush := prMergeGitPush
	origMerge := autoPRGhMerge
	prMergeGitPush = func(repoPath, ref, token string) error {
		f.prPushes = append(f.prPushes, autoPRPushArgs{repoPath, ref, token})
		return nil
	}
	autoPRGhMerge = func(repoPath, head, token string) error {
		f.ghMerge = append(f.ghMerge, prMergeGhArgs{repoPath, head, token})
		return f.mergeErr
	}
	return func() {
		prMergeGitPush = origPush
		autoPRGhMerge = origMerge
	}
}

func TestAutoPR_Disabled(t *testing.T) {
	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/1"}
	restore := installAutoPRFake(fake)
	defer restore()
	bc := &autoPRFakeBeadsClient{}
	bead := &types.Bead{ID: "proj-abc", Title: "Add foo"}
	maybeOpenAutoPR(bc, &Config{Output: OutputConfig{Push: true}}, "/tmp", "", "main", bead)
	if len(fake.pushes)+len(fake.lsRemote)+len(fake.ghCreate) > 0 {
		t.Fatalf("expected no auto-pr activity when disabled, got pushes=%d ls=%d gh=%d", len(fake.pushes), len(fake.lsRemote), len(fake.ghCreate))
	}
}

func TestAutoPR_OpensPR(t *testing.T) {
	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/42"}
	restore := installAutoPRFake(fake)
	defer restore()
	bc := &autoPRFakeBeadsClient{}
	bead := &types.Bead{ID: "proj-abc", Title: "Add foo", Labels: []string{"fr"}}
	maybeOpenAutoPR(bc, &Config{Output: OutputConfig{Push: true, AutoPR: true}}, "/tmp", "", "main", bead)
	if len(fake.prPushes) != 1 {
		t.Fatalf("expected 1 force push, got %d", len(fake.prPushes))
	}
	if fake.prPushes[0].ref != "refs/heads/dogfood/proj-abc" {
		t.Errorf("push ref = %q, want refs/heads/dogfood/proj-abc", fake.prPushes[0].ref)
	}
	if len(fake.ghCreate) != 1 {
		t.Fatalf("expected 1 gh create, got %d", len(fake.ghCreate))
	}
	g := fake.ghCreate[0]
	if g.base != "main" || g.head != "dogfood/proj-abc" || g.title != "Add foo" || g.body != "Authored by a Hydra dogfood run." {
		t.Errorf("unexpected gh args: %+v", g)
	}
	if len(fake.ghMerge) != 1 {
		t.Fatalf("expected the PR to be merged after creation, got %d merges", len(fake.ghMerge))
	}
	wantLabels := []string{"fr", "pr-open"}
	if got := bc.labels["proj-abc"]; !sliceStrEqual(got, wantLabels) {
		t.Errorf("labels = %v, want %v", got, wantLabels)
	}
	if desc := bc.descriptions["proj-abc"]; !strings.Contains(desc, "https://github.com/x/y/pull/42") {
		t.Errorf("description missing URL: %q", desc)
	}
}

func TestAutoPR_SkipsWhenLabelPresent(t *testing.T) {
	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/42"}
	restore := installAutoPRFake(fake)
	defer restore()
	bc := &autoPRFakeBeadsClient{}
	bead := &types.Bead{ID: "proj-abc", Title: "Add foo", Labels: []string{"pr-open"}}
	maybeOpenAutoPR(bc, &Config{Output: OutputConfig{Push: true, AutoPR: true}}, "/tmp", "", "main", bead)
	if len(fake.pushes)+len(fake.lsRemote)+len(fake.ghCreate) > 0 {
		t.Fatalf("expected no auto-pr activity for already-open bead, got pushes=%d ls=%d gh=%d", len(fake.pushes), len(fake.lsRemote), len(fake.ghCreate))
	}
}

// TestAutoPR_MergesExistingOpenPR: a leftover dogfood branch WITH an open PR
// tracking it is the hydra-f40k strand signature — the earlier integration
// pushed the branch but never merged. The fallback must land it now (verified
// MERGED), not skip.
func TestAutoPR_MergesExistingOpenPR(t *testing.T) {
	fake := &autoPRFakeExec{lsFound: true, listOpen: true}
	restore := installAutoPRFake(fake)
	defer restore()
	bc := &autoPRFakeBeadsClient{}
	bead := &types.Bead{ID: "proj-abc", Title: "Add foo"}
	maybeOpenAutoPR(bc, &Config{Output: OutputConfig{Push: true, AutoPR: true}}, "/tmp", "", "main", bead)
	if len(fake.ghMerge) != 1 {
		t.Fatalf("expected the existing open PR to be merged, got %d merges", len(fake.ghMerge))
	}
	if fake.ghMerge[0].head != "dogfood/proj-abc" {
		t.Errorf("merge head = %q, want dogfood/proj-abc", fake.ghMerge[0].head)
	}
	if len(fake.prPushes) > 0 || len(fake.ghCreate) > 0 {
		t.Fatalf("expected no force-push or PR create when an open PR already tracks the branch, got pushes=%d creates=%d", len(fake.prPushes), len(fake.ghCreate))
	}
}

// TestAutoPR_RecoversBranchWithoutOpenPR: a leftover dogfood branch with NO
// open PR is a stranded artifact — the 2026-09-25 f40k incident. The fallback
// must refresh the branch and open + merge the PR, not skip.
func TestAutoPR_RecoversBranchWithoutOpenPR(t *testing.T) {
	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/43", lsFound: true, listOpen: false}
	restore := installAutoPRFake(fake)
	defer restore()
	bc := &autoPRFakeBeadsClient{}
	bead := &types.Bead{ID: "proj-abc", Title: "Add foo", Labels: []string{"fr"}}
	maybeOpenAutoPR(bc, &Config{Output: OutputConfig{Push: true, AutoPR: true}}, "/tmp", "", "main", bead)
	if len(fake.prPushes) != 1 || fake.prPushes[0].ref != "refs/heads/dogfood/proj-abc" {
		t.Fatalf("expected force push of refs/heads/dogfood/proj-abc, got %+v", fake.prPushes)
	}
	if len(fake.ghCreate) != 1 || len(fake.ghMerge) != 1 {
		t.Fatalf("expected create + merge, got creates=%d merges=%d", len(fake.ghCreate), len(fake.ghMerge))
	}
	wantLabels := []string{"fr", "pr-open"}
	if got := bc.labels["proj-abc"]; !sliceStrEqual(got, wantLabels) {
		t.Errorf("labels = %v, want %v", got, wantLabels)
	}
	if desc := bc.descriptions["proj-abc"]; !strings.Contains(desc, "https://github.com/x/y/pull/43") {
		t.Errorf("description missing URL: %q", desc)
	}
}

// TestAutoPR_FailClosedWhenMergeFails: a created-but-unmerged PR must NOT
// count as integration preserved — the caller must still see the failure.
func TestAutoPR_FailClosedWhenMergeFails(t *testing.T) {
	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/44", mergeErr: errors.New("merge conflict")}
	restore := installAutoPRFake(fake)
	defer restore()
	bc := &autoPRFakeBeadsClient{}
	bead := &types.Bead{ID: "proj-abc", Title: "Add foo"}
	maybeOpenAutoPR(bc, &Config{Output: OutputConfig{Push: true, AutoPR: true}}, "/tmp", "", "main", bead)
	if len(fake.ghCreate) != 1 || len(fake.ghMerge) != 1 {
		t.Fatalf("expected create + merge attempt, got creates=%d merges=%d", len(fake.ghCreate), len(fake.ghMerge))
	}
	if len(bc.labels) > 0 {
		t.Fatalf("expected no label update on unmerged PR, got %v", bc.labels)
	}
	if len(bc.descriptions) > 0 {
		t.Fatalf("expected no description update on unmerged PR, got %v", bc.descriptions)
	}
}

func TestAutoPR_FailOpenOnGhError(t *testing.T) {
	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/42", ghErr: errors.New("gh exploded")}
	restore := installAutoPRFake(fake)
	defer restore()
	bc := &autoPRFakeBeadsClient{}
	bead := &types.Bead{ID: "proj-abc", Title: "Add foo"}
	maybeOpenAutoPR(bc, &Config{Output: OutputConfig{Push: true, AutoPR: true}}, "/tmp", "", "main", bead)
	if len(fake.prPushes) != 1 {
		t.Fatalf("expected force push to happen, got %d", len(fake.prPushes))
	}
	if len(bc.labels) > 0 {
		t.Fatalf("expected no label update on gh failure, got %v", bc.labels)
	}
	if len(bc.descriptions) > 0 {
		t.Fatalf("expected no description update on gh failure, got %v", bc.descriptions)
	}
}

// TestPublishAfterIntegrate_AutoPRWhenVerificationFails ensures that a push
// which appears to succeed but does not actually update the remote (e.g. branch
// protection silently drops the pack) is caught by verification and falls back
// to the auto-PR path.
func TestPublishAfterIntegrate_AutoPRWhenVerificationFails(t *testing.T) {
	repo, remote := setupReachableRemote(t)
	commitFile(t, repo, "file.txt", "a")
	git(t, repo, "push", "-u", "origin", "main")
	first := headCommit(t, repo)

	commitFile(t, repo, "file.txt", "b")

	hook := fmt.Sprintf("#!/bin/sh\nwhile read old new ref; do\n  case \"$ref\" in\n    refs/heads/main)\n      git update-ref refs/heads/main %s \"$new\"\n      ;;\n  esac\ndone\n", first)
	writeHook(t, remote, "post-receive", hook)

	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/99"}
	restore := installAutoPRFake(fake)
	defer restore()

	bc := &autoPRFakeBeadsClient{}
	closer := NewBeadCloser(bc, nil, nil, nil, &Config{Output: OutputConfig{Push: true, AutoPR: true}})
	closer.SetWorktreeManager(NewWorktreeManager(), nil, nil)
	closer.publishAfterIntegrate(repo, &types.Bead{ID: "bead-xyz", Title: "Fix things"})

	if len(fake.prPushes) != 1 {
		t.Fatalf("expected auto-pr force push, got %d", len(fake.prPushes))
	}
	if fake.prPushes[0].ref != "refs/heads/dogfood/bead-xyz" {
		t.Errorf("unexpected auto-pr ref %q", fake.prPushes[0].ref)
	}
	if len(fake.ghCreate) != 1 {
		t.Fatalf("expected gh pr create, got %d", len(fake.ghCreate))
	}
	if fake.ghCreate[0].base != "main" || fake.ghCreate[0].head != "dogfood/bead-xyz" {
		t.Errorf("unexpected gh args: %+v", fake.ghCreate[0])
	}
}

func sliceStrEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPublishAfterIntegrate_PRMergeSkipsDirectPush: with output.pr_merge on
// and no branch protection in the way, a direct push would succeed — so the
// assertion is negative on the remote: main must NOT advance via PushBranch,
// and the PR-merge hooks must carry the integrate.
func TestPublishAfterIntegrate_PRMergeSkipsDirectPush(t *testing.T) {
	repo, remote := setupReachableRemote(t)
	commitFile(t, repo, "file.txt", "a")
	git(t, repo, "push", "-u", "origin", "main")
	remoteMain := git(t, remote, "rev-parse", "refs/heads/main")
	commitFile(t, repo, "file.txt", "b")

	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/7"}
	restore := installAutoPRFake(fake)
	restoreMerge := installPRMergeFake(fake)
	defer restore()
	defer restoreMerge()

	closer := NewBeadCloser(&autoPRFakeBeadsClient{}, nil, nil, nil,
		&Config{Output: OutputConfig{Push: true, PRMerge: true}})
	closer.SetWorktreeManager(NewWorktreeManager(), nil, nil)
	if err := closer.publishAfterIntegrate(repo, &types.Bead{ID: "bead-7", Title: "Fix things"}); err != nil {
		t.Fatalf("publishAfterIntegrate: %v", err)
	}

	if git(t, remote, "rev-parse", "refs/heads/main") != remoteMain {
		t.Fatalf("direct push advanced remote main — PR route must not push main directly")
	}
	if len(fake.prPushes) != 1 || fake.prPushes[0].ref != "refs/heads/dogfood/bead-7" {
		t.Fatalf("expected force push to dogfood/bead-7, got %+v", fake.prPushes)
	}
	if len(fake.ghCreate) != 1 || fake.ghCreate[0].base != "main" || fake.ghCreate[0].head != "dogfood/bead-7" {
		t.Fatalf("unexpected gh create args: %+v", fake.ghCreate)
	}
	if len(fake.ghMerge) != 1 || fake.ghMerge[0].head != "dogfood/bead-7" {
		t.Fatalf("expected gh pr merge for dogfood/bead-7, got %+v", fake.ghMerge)
	}
}

// TestPublishAfterIntegrate_PRMergeFailsClosed: an unmerged PR (merge hook
// error — e.g. gh exit 1 from branch protection) must surface as a push
// error so the close aborts and the worktree survives.
func TestPublishAfterIntegrate_PRMergeFailsClosed(t *testing.T) {
	repo, _ := setupReachableRemote(t)
	commitFile(t, repo, "file.txt", "a")
	git(t, repo, "push", "-u", "origin", "main")
	commitFile(t, repo, "file.txt", "b")

	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/8", mergeErr: errors.New("gh pr merge: GraphQL: Protected branch")}
	restore := installAutoPRFake(fake)
	restoreMerge := installPRMergeFake(fake)
	defer restore()
	defer restoreMerge()

	closer := NewBeadCloser(&autoPRFakeBeadsClient{}, nil, nil, nil,
		&Config{Output: OutputConfig{Push: true, PRMerge: true}})
	closer.SetWorktreeManager(NewWorktreeManager(), nil, nil)
	err := closer.publishAfterIntegrate(repo, &types.Bead{ID: "bead-8", Title: "Fix things"})
	if err == nil {
		t.Fatal("expected error when PR merge fails, got nil")
	}
	if !strings.Contains(err.Error(), "Protected branch") {
		t.Errorf("error must carry the gh failure verbatim, got: %v", err)
	}
	if len(fake.ghMerge) != 1 {
		t.Fatalf("expected one merge attempt, got %d", len(fake.ghMerge))
	}
}

// TestPublishRepo_PRMergeRoutesViaPR: the periodic publisher with pr_merge
// on must not direct-push the working branch; it goes through the PR hooks.
func TestPublishRepo_PRMergeRoutesViaPR(t *testing.T) {
	base := t.TempDir()
	bare := filepath.Join(base, "remote.git")
	repo := filepath.Join(base, "work")
	gitRun(t, base, "init", "--bare", bare)
	gitRun(t, base, "clone", bare, repo)
	gitRun(t, repo, "config", "user.email", "t@e.co")
	gitRun(t, repo, "config", "user.name", "T")
	gitRun(t, repo, "commit", "--allow-empty", "-m", "c1")
	gitRun(t, repo, "push", "origin", "master")

	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/9"}
	restore := installAutoPRFake(fake)
	restoreMerge := installPRMergeFake(fake)
	defer restore()
	defer restoreMerge()

	commitFile := filepath.Join(repo, "more.txt")
	if err := os.WriteFile(commitFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "c2")

	pushed, reason := PublishRepo(NewWorktreeManager(), repo, "", true, nil, func(string) bool { return true })
	if !pushed {
		t.Fatalf("expected PR-route publish, got (false, %q)", reason)
	}
	if len(fake.prPushes) != 1 || fake.prPushes[0].ref != "refs/heads/dogfood/publish-work" {
		t.Fatalf("expected force push to dogfood/publish-work, got %+v", fake.prPushes)
	}
	if len(fake.ghMerge) != 1 {
		t.Fatalf("expected gh pr merge, got %+v", fake.ghMerge)
	}
	// Direct push must not have landed on the remote either.
	out, err := exec.Command("git", "-C", bare, "rev-list", "--count", "master").Output()
	if err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Fatalf("remote master advanced without a PR: count=%q err=%v", strings.TrimSpace(string(out)), err)
	}
}

// TestPublishViaPR_PropagatesCreateFailure: a PR that never gets created
// returns no URL and the push error, so nothing downstream can claim success.
func TestPublishViaPR_PropagatesCreateFailure(t *testing.T) {
	fake := &autoPRFakeExec{ghErr: errors.New("gh pr create: HTTP 403")}
	restore := installAutoPRFake(fake)
	restoreMerge := installPRMergeFake(fake)
	defer restore()
	defer restoreMerge()

	url, err := PublishViaPR("/tmp/x", "main", "dogfood/z", "T", "")
	if err == nil || url != "" {
		t.Fatalf("expected error + empty URL, got (%q, %v)", url, err)
	}
	if len(fake.ghMerge) != 0 {
		t.Fatalf("merge must not run when PR creation failed, got %+v", fake.ghMerge)
	}
}

// TestPublishAfterIntegrate_PRTipAlreadyMerged_NoOp: when the bead's tip is
// already contained in the remote base branch (a racing duplicate close
// landed it first — hydra-kf2t, 2026-09-27), the second integrate attempt
// must no-op instead of creating a second PR.
func TestPublishAfterIntegrate_PRTipAlreadyMerged_NoOp(t *testing.T) {
	repo, _ := setupReachableRemote(t)
	commitFile(t, repo, "file.txt", "a")
	git(t, repo, "push", "-u", "origin", "main")

	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/9"}
	restore := installAutoPRFake(fake)
	restoreMerge := installPRMergeFake(fake)
	defer restore()
	defer restoreMerge()

	origProbe := tipMergedProbe
	tipMergedProbe = func(string, string) bool { return true }
	defer func() { tipMergedProbe = origProbe }()

	closer := NewBeadCloser(&autoPRFakeBeadsClient{}, nil, nil, nil,
		&Config{Output: OutputConfig{Push: true, PRMerge: true}})
	closer.SetWorktreeManager(NewWorktreeManager(), nil, nil)
	if err := closer.publishAfterIntegrate(repo, &types.Bead{ID: "bead-9", Title: "Fix things"}); err != nil {
		t.Fatalf("duplicate integrate must no-op, got: %v", err)
	}
	if len(fake.ghCreate) != 0 {
		t.Fatalf("no PR may be created for an already-landed tip, got %+v", fake.ghCreate)
	}
}

// TestPublishAfterIntegrate_PRNoCommitsBetween_Benign: when gh pr create
// rejects with "No commits between" (the winning close's PR merge landed the
// same tip between our pre-check and the create), the publish must be
// treated as landed, not failed — no integration-failed reopen, no scary
// FAILED log path.
func TestPublishAfterIntegrate_PRNoCommitsBetween_Benign(t *testing.T) {
	repo, _ := setupReachableRemote(t)
	commitFile(t, repo, "file.txt", "a")
	git(t, repo, "push", "-u", "origin", "main")
	// Move the local tip past origin/main so the real probe reports
	// not-yet-landed and the run reaches gh pr create.
	commitFile(t, repo, "file.txt", "b")

	fake := &autoPRFakeExec{ghErr: errors.New("gh pr create: exit status 1\npull request create failed: GraphQL: No commits between development and dogfood/bead-10 (createPullRequest)")}
	restore := installAutoPRFake(fake)
	restoreMerge := installPRMergeFake(fake)
	defer restore()
	defer restoreMerge()

	closer := NewBeadCloser(&autoPRFakeBeadsClient{}, nil, nil, nil,
		&Config{Output: OutputConfig{Push: true, PRMerge: true}})
	closer.SetWorktreeManager(NewWorktreeManager(), nil, nil)
	if err := closer.publishAfterIntegrate(repo, &types.Bead{ID: "bead-10", Title: "Fix things"}); err != nil {
		t.Fatalf("no-commits-between must be a benign no-op, got: %v", err)
	}
	if len(fake.ghCreate) != 1 {
		t.Fatalf("expected one create attempt before the benign classification, got %d", len(fake.ghCreate))
	}
}

// TestDefaultPRMergeGitPush_SkipsPrePushHooks proves daemon dogfood pushes
// bypass client-side pre-push hooks (husky lint:ci && build): a hook that
// always fails must not block the push — the daemon already ran its own
// gates, and a hook failure strands verified closes (2026-09-27: 64
// telematics commits stranded locally on an eslint error in integrated work).
func TestDefaultPRMergeGitPush_SkipsPrePushHooks(t *testing.T) {
	repo, remote := setupReachableRemote(t)
	commitFile(t, repo, "file.txt", "a")
	hookPath := filepath.Join(repo, ".git", "hooks", "pre-push")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write pre-push hook: %v", err)
	}
	if err := defaultPRMergeGitPush(repo, "refs/heads/dogfood/test", ""); err != nil {
		t.Fatalf("push with failing pre-push hook must succeed via --no-verify, got: %v", err)
	}
	// Sanity: the hook without --no-verify really would have failed.
	if _, err := gitOut(repo, "push", "--force", "origin", "HEAD:refs/heads/dogfood/test2"); err == nil {
		t.Fatal("control push without --no-verify unexpectedly succeeded — hook not armed")
	}
	if out, err := gitOut(remote, "rev-parse", "refs/heads/dogfood/test"); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("dogfood branch missing on remote after push: %v", err)
	}
}

// TestDefaultPRMergeGitPush_RedactsToken proves the authenticated push URL's
// embedded PAT never reaches error strings (2026-09-27: a failing pre-push
// hook put the full token into the daemon log 906 times).
func TestDefaultPRMergeGitPush_RedactsToken(t *testing.T) {
	repo := setupLocalRepo(t)
	commitFile(t, repo, "file.txt", "a")
	gitRun(t, repo, "remote", "add", "origin", "https://127.0.0.1:1/x/y.git")
	err := defaultPRMergeGitPush(repo, "refs/heads/dogfood/test", "SECRETTOKEN123")
	if err == nil {
		t.Fatal("expected push to unreachable host to fail")
	}
	if strings.Contains(err.Error(), "SECRETTOKEN123") {
		t.Fatalf("error leaked the token: %v", err)
	}
	if !strings.Contains(err.Error(), "***") {
		t.Fatalf("expected redacted placeholder in error, got: %v", err)
	}
}

// TestPublishViaPR_FallsBackToDaemonToken proves an empty caller token (the
// periodic publish path only carries the project owner's PAT, absent for
// daemon-owned repos) resolves the daemon credential instead of invoking gh
// unauthenticated (2026-09-28: publish tick looped on gh pr create exit
// status 4 for the hydra repo).
func TestPublishViaPR_FallsBackToDaemonToken(t *testing.T) {
	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/9"}
	restore := installPRMergeFake(fake)
	defer restore()
	origCreate := autoPRGhCreate
	autoPRGhCreate = func(repoPath, base, head, title, body, token string) (string, error) {
		fake.ghCreate = append(fake.ghCreate, autoPRGhArgs{repoPath, base, head, title, body, token})
		return fake.ghURL, fake.ghErr
	}
	defer func() { autoPRGhCreate = origCreate }()
	origResolve := prMergeResolveToken
	prMergeResolveToken = func() string { return "FALLBACK_TOK" }
	defer func() { prMergeResolveToken = origResolve }()

	url, err := PublishViaPR("/tmp/x", "main", "dogfood/publish-x", "t", "")
	if err != nil {
		t.Fatalf("PublishViaPR: %v", err)
	}
	if url == "" {
		t.Fatal("expected PR URL")
	}
	if len(fake.prPushes) != 1 || fake.prPushes[0].token != "FALLBACK_TOK" {
		t.Fatalf("push token = %+v, want FALLBACK_TOK", fake.prPushes)
	}
	if len(fake.ghCreate) != 1 || fake.ghCreate[0].token != "FALLBACK_TOK" {
		t.Fatalf("create token = %+v, want FALLBACK_TOK", fake.ghCreate)
	}
	if len(fake.ghMerge) != 1 || fake.ghMerge[0].token != "FALLBACK_TOK" {
		t.Fatalf("merge token = %+v, want FALLBACK_TOK", fake.ghMerge)
	}
}
