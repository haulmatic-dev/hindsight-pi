package core

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// newPubBaseRepo builds a scratch git repo with a bare origin carrying a
// development branch. Returns the repo path. Caller removes with dir cleanup.
func newPubBaseRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	repo := filepath.Join(root, "repo")
	if out, err := exec.Command("git", "init", "--bare", origin).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("init repo: %v: %s", err, out)
	}
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	run("remote", "add", "origin", origin)
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("checkout", "-b", "development")
	run("commit", "--allow-empty", "-m", "base")
	run("push", "-q", "origin", "development")
	return repo
}

// TestBranchMergedIntoBase pins the hydra-ybhh publish-loop guard: a working
// branch whose tip is already merged into origin/<base> (or whose remote
// branch was pruned after the merge) must be detected so PublishRepo skips
// the doomed no-op PR creation (GraphQL blank head/base sha, "No commits
// between").
func TestBranchMergedIntoBase(t *testing.T) {
	repo := newPubBaseRepo(t)
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	// Branch merged upstream then its remote ref pruned (PR #144 pattern).
	run("checkout", "-b", "dogfood/orphan-digest-escalated")
	run("commit", "--allow-empty", "-m", "strand work")
	run("checkout", "development")
	run("merge", "--ff-only", "dogfood/orphan-digest-escalated")
	run("push", "-q", "origin", "development")
	run("branch", "-f", "dogfood/orphan-digest-escalated", "HEAD") // tip = merged commit
	if !branchMergedIntoBase(repo, "dogfood/orphan-digest-escalated") {
		t.Fatal("merged branch tip must be reported already integrated")
	}

	// Branch with real unmerged work must NOT be skipped.
	run("checkout", "-b", "dogfood/realwork")
	run("commit", "--allow-empty", "-m", "unmerged strand")
	run("checkout", "development")
	if branchMergedIntoBase(repo, "dogfood/realwork") {
		t.Fatal("unmerged branch must not be reported already integrated")
	}

	// base == branch or empty project path are no-ops (fail-open).
	if branchMergedIntoBase(repo, "development") {
		t.Fatal("branch == base must return false")
	}
	if branchMergedIntoBase("", "development") || branchMergedIntoBase(repo, "") {
		t.Fatal("empty inputs must return false")
	}

}
