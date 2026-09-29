package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// setupQuarantineRepo builds a real git repo with a working branch (main)
// and a hydra/quarantine/<key> branch. conflicted=true creates a merge
// conflict between the two.
func setupQuarantineRepo(t *testing.T, key string, conflicted bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := gitOut(dir, args...); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-q", "-b", "main", ".")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("f.txt", "base\n")
	run("add", "-A")
	run("commit", "-qm", "base")

	run("checkout", "-qb", quarantinePrefix+key)
	if conflicted {
		write("f.txt", "quarantine\n")
	} else {
		write("q-only.txt", "new work\n")
	}
	run("add", "-A")
	run("commit", "-qm", "quarantine work")

	run("checkout", "-q", "main")
	if conflicted {
		write("f.txt", "moved base\n")
		run("add", "-A")
		run("commit", "-qm", "base moved")
	}
	return dir
}

type fakeReattemptBeads struct {
	BeadsClient
	labels  []string
	status  types.BeadStatus
	removed bool
}

func (f *fakeReattemptBeads) GetBead(string, string, types.BeadID) (*types.Bead, error) {
	return &types.Bead{ID: "k-1", Status: f.status}, nil
}
func (f *fakeReattemptBeads) RemoveBeadLabels(string, string, types.BeadID, []string) error {
	f.removed = true
	return nil
}
func (f *fakeReattemptBeads) UpdateBeadStatus(string, types.BeadID, types.BeadStatus) error {
	f.status = types.StatusOpen
	return nil
}

type captureSink struct{ events []Event }

func (c *captureSink) EmitRepo(_ string, ev Event) Event {
	c.events = append(c.events, ev)
	return ev
}

func TestQuarantineReattempt_StillConflictedRefused(t *testing.T) {
	dir := setupQuarantineRepo(t, "k-1", true)
	err := QuarantineReattemptRepair(dir, "k-1", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "still conflicts") {
		t.Fatalf("want still-conflicts refusal, got %v", err)
	}
	if out, qerr := gitOut(dir, "rev-parse", "--verify", "refs/heads/"+quarantinePrefix+"k-1"); qerr != nil {
		t.Fatalf("quarantine branch must survive refusal: %v %s", qerr, out)
	}
}

func TestQuarantineReattempt_CleanRequeues(t *testing.T) {
	dir := setupQuararantineClean(t)
	beads := &fakeReattemptBeads{status: types.StatusInProgress}
	sink := &captureSink{}
	if err := QuarantineReattemptRepair(dir, "k-1", beads, sink); err != nil {
		t.Fatalf("clean re-attempt failed: %v", err)
	}
	// Worktree branch restored at the quarantine tip.
	wt := (&WorktreeManager{}).branchName("k-1")
	if _, err := gitOut(dir, "rev-parse", "--verify", "refs/heads/"+wt); err != nil {
		t.Fatalf("worktree branch %s not restored: %v", wt, err)
	}
	if _, err := gitOut(dir, "rev-parse", "--verify", "refs/heads/"+quarantinePrefix+"k-1"); err == nil {
		t.Fatal("quarantine branch should be deleted after requeue")
	}
	if !beads.removed {
		t.Fatal("stranded-conflict label not removed")
	}
	if beads.status != types.StatusOpen {
		t.Fatalf("bead status = %s, want open", beads.status)
	}
	if len(sink.events) != 1 || sink.events[0].Type != "quarantine_reattempt" {
		t.Fatalf("want one quarantine_reattempt event, got %+v", sink.events)
	}
}

// helper split out only to keep the setup name readable.
func setupQuararantineClean(t *testing.T) string { return setupQuarantineRepo(t, "k-1", false) }

func TestQuarantineReattempt_NoQuarantineBranch(t *testing.T) {
	dir := setupQuarantineRepo(t, "k-1", false)
	if _, err := gitOut(dir, "branch", "-D", quarantinePrefix+"k-1"); err != nil {
		t.Fatal(err)
	}
	err := QuarantineReattemptRepair(dir, "k-1", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no quarantine branch") {
		t.Fatalf("want no-quarantine-branch error, got %v", err)
	}
}
