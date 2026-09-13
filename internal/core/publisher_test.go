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
func (m *autoPRFakeBeadsClient) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	if m.descriptions == nil {
		m.descriptions = make(map[string]string)
	}
	m.descriptions[string(beadID)] = description
	return nil
}

type autoPRFakeExec struct {
	pushes    []autoPRPushArgs
	lsRemote  []autoPRLsArgs
	ghCreate  []autoPRGhArgs
	prPushes  []autoPRPushArgs
	ghMerge   []prMergeGhArgs
	ghURL     string
	ghErr     error
	mergeErr  error
	lsFound   bool
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
	return func() {
		autoPRGitPush = origPush
		autoPRLsRemote = origLs
		autoPRGhCreate = origGh
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
	if len(fake.pushes) != 1 {
		t.Fatalf("expected 1 push, got %d", len(fake.pushes))
	}
	if fake.pushes[0].ref != "refs/heads/dogfood/proj-abc" {
		t.Errorf("push ref = %q, want refs/heads/dogfood/proj-abc", fake.pushes[0].ref)
	}
	if len(fake.ghCreate) != 1 {
		t.Fatalf("expected 1 gh create, got %d", len(fake.ghCreate))
	}
	g := fake.ghCreate[0]
	if g.base != "main" || g.head != "dogfood/proj-abc" || g.title != "Add foo" || g.body != "Authored by a Hydra dogfood run." {
		t.Errorf("unexpected gh args: %+v", g)
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

func TestAutoPR_SkipsWhenRemoteBranchExists(t *testing.T) {
	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/42", lsFound: true}
	restore := installAutoPRFake(fake)
	defer restore()
	bc := &autoPRFakeBeadsClient{}
	bead := &types.Bead{ID: "proj-abc", Title: "Add foo"}
	maybeOpenAutoPR(bc, &Config{Output: OutputConfig{Push: true, AutoPR: true}}, "/tmp", "", "main", bead)
	if len(fake.pushes) > 0 {
		t.Fatalf("expected no push when remote branch exists, got %d", len(fake.pushes))
	}
	if len(fake.ghCreate) > 0 {
		t.Fatalf("expected no gh create when remote branch exists, got %d", len(fake.ghCreate))
	}
}

func TestAutoPR_FailOpenOnGhError(t *testing.T) {
	fake := &autoPRFakeExec{ghURL: "https://github.com/x/y/pull/42", ghErr: errors.New("gh exploded")}
	restore := installAutoPRFake(fake)
	defer restore()
	bc := &autoPRFakeBeadsClient{}
	bead := &types.Bead{ID: "proj-abc", Title: "Add foo"}
	maybeOpenAutoPR(bc, &Config{Output: OutputConfig{Push: true, AutoPR: true}}, "/tmp", "", "main", bead)
	if len(fake.pushes) != 1 {
		t.Fatalf("expected push to happen, got %d", len(fake.pushes))
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

	if len(fake.pushes) != 1 {
		t.Fatalf("expected auto-pr push, got %d", len(fake.pushes))
	}
	if fake.pushes[0].ref != "refs/heads/dogfood/bead-xyz" {
		t.Errorf("unexpected auto-pr ref %q", fake.pushes[0].ref)
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
