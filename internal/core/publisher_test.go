package core

import (
	"os/exec"
	"path/filepath"
	"testing"
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
	if ok, reason := PublishRepo(nil, "/x", "tok", nil); ok || reason != "no worktree manager" {
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

	pushed, reason := PublishRepo(NewWorktreeManager(), repo, "", nil)
	if !pushed {
		t.Fatalf("expected push, got (false, %q)", reason)
	}
	// Commit must now exist on the bare remote.
	out, err := exec.Command("git", "-C", bare, "log", "--oneline").CombinedOutput()
	if err != nil || len(out) == 0 {
		t.Fatalf("remote has no commits after publish: %v\n%s", err, out)
	}
}
