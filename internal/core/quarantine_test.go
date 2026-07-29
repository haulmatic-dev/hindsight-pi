package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func initQuarantineRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "base")
	return dir
}

// TestQuarantineAndRemove_PreservesWorkOffTheWorkingBranch pins A1's contract:
// a failed chain's commits end up on hydra/quarantine/<key>, the working branch
// stays untouched, and the worktree is gone.
func TestQuarantineAndRemove_PreservesWorkOffTheWorkingBranch(t *testing.T) {
	repo := initQuarantineRepo(t)
	m := NewWorktreeManager()

	wt, err := m.EnsureWorktree(repo, "chain1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// A failing test lands in the chain worktree — the exact artifact that
	// previously leaked onto the working branch.
	if err := os.WriteFile(filepath.Join(wt, "red_test.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if committed, err := m.Commit(wt, "chain work with red test"); err != nil || !committed {
		t.Fatalf("Commit: committed=%v err=%v", committed, err)
	}

	qBranch, err := m.QuarantineAndRemove(repo, "chain1")
	if err != nil {
		t.Fatalf("QuarantineAndRemove: %v", err)
	}
	if qBranch != "hydra/quarantine/chain1" {
		t.Errorf("qBranch = %q", qBranch)
	}

	// Working branch clean of the red test.
	if _, err := os.Stat(filepath.Join(repo, "red_test.go")); !os.IsNotExist(err) {
		t.Error("red test leaked onto the working branch")
	}
	// Work preserved on the quarantine branch.
	out, err := exec.Command("git", "-C", repo, "show", qBranch+":red_test.go").Output()
	if err != nil || !strings.Contains(string(out), "package x") {
		t.Errorf("quarantined work not preserved: %v %q", err, out)
	}
	// Chain branch and worktree gone.
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "hydra/wt-chain1").Run(); err == nil {
		t.Error("chain branch should be deleted after quarantine")
	}
	// Idempotent: nothing left to quarantine.
	if q2, err := m.QuarantineAndRemove(repo, "chain1"); err != nil || q2 != "" {
		t.Errorf("second quarantine = (%q, %v), want empty no-op", q2, err)
	}
}

// TestChainHasFailedBeads reads the closed-failed label the closer writes.
func TestChainHasFailedBeads(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, ".beads", "issues.jsonl")
	if err := os.MkdirAll(filepath.Dir(jsonl), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"id":"p-ok","status":"closed","labels":[]}` + "\n" +
		`{"id":"p-bad","status":"closed","labels":["auto-fix","closed-failed"]}` + "\n"
	if err := os.WriteFile(jsonl, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	o := &Overseer{beadFileReader: NewBeadFileReader([]types.Project{{Name: "p", Path: dir, Type: types.RepoTypeSingle}})}
	d := &ChainCompletionDetector{overseer: o}

	if d.ChainHasFailedBeads([]types.BeadID{"p-ok"}) {
		t.Error("clean chain flagged as failed")
	}
	if !d.ChainHasFailedBeads([]types.BeadID{"p-ok", "p-bad"}) {
		t.Error("chain with a closed-failed member must be flagged")
	}
	if d.ChainHasFailedBeads([]types.BeadID{"p-missing"}) {
		t.Error("missing beads must not flag the chain")
	}
}
