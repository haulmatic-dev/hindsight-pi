package core

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Post-close verification sweep contracts (RED phase — internal/core/postclose_sweep.go
// does not exist yet; these tests pin the required behavior).
//
// Sweep contract:
//   - Only recently closed beads (UpdatedAt within 24h) carrying a
//     metadata["test_command"] are re-verified in the project's default-branch
//     checkout. Older beads and beads without commands are skipped.
//   - Flake guard: a failing verification is re-run once. If the re-run
//     passes, the failure was a flake — the bead gets the `postclose-flake`
//     label and is NOT reopened.
//   - Reopen path: both failing-run outputs are attached (as a comment), the
//     bead is reopened (status open) and labeled `postclose-reopened`.
//   - Bounded auto-reopen: a bead already carrying `postclose-reopened` that
//     fails again gets `needs-human` added instead of a second reopen.

const (
	pcProjectsName = "demo"
)

func pcProjects(path string) []types.Project {
	return []types.Project{{Name: pcProjectsName, Path: path, Type: types.RepoTypeSingle}}
}

// pcFakeBeads is a minimal BeadsClient double recording mutations.
type pcFakeBeads struct {
	BeadsClient
	beads    []types.Bead
	statuses map[string]types.BeadStatus
	labels   map[string][]string
	listErr  error
}

func (f *pcFakeBeads) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.beads, nil
}

func (f *pcFakeBeads) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	f.statuses[beadID.String()] = status
	return nil
}

func (f *pcFakeBeads) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	f.labels[beadID.String()] = append(f.labels[beadID.String()], labels...)
	return nil
}

func newPCFakeBeads(beads []types.Bead) *pcFakeBeads {
	return &pcFakeBeads{beads: beads, statuses: map[string]types.BeadStatus{}, labels: map[string][]string{}}
}

// pcRunner builds a runCommand func returning canned results in order.
func pcRunner(results ...struct {
	output string
	err    bool
}) func(string, string) (string, error) {
	i := 0
	return func(projectPath, command string) (string, error) {
		if i >= len(results) {
			return "unexpected extra invocation", nil
		}
		r := results[i]
		i++
		if r.err {
			return r.output, &exec.ExitError{}
		}
		return r.output, nil
	}
}

func pcClosedBead(id, command string, age time.Duration, labels ...string) types.Bead {
	return types.Bead{
		ID:        types.BeadID(id),
		Title:     "closed work " + id,
		Status:    types.StatusClosed,
		Labels:    labels,
		UpdatedAt: time.Now().Add(-age),
		Metadata: map[string]string{
			"test_command": command,
		},
	}
}

// Reopen path: failing command (both runs fail) → reopened, labeled, comment
// with BOTH outputs attached.
func TestPostCloseSweepReopensAndAttachesBothOutputs(t *testing.T) {
	fake := newPCFakeBeads([]types.Bead{
		pcClosedBead("demo-re1", "go test ./...", time.Hour),
	})
	var commentBody string
	commenter := func(projectPath string, beadID types.BeadID, body string) error {
		commentBody = body
		return nil
	}
	runner := pcRunner(
		struct {
			output string
			err    bool
		}{"FAIL run1: exit status 1", true},
		struct {
			output string
			err    bool
		}{"FAIL run2: exit status 1", true},
	)
	s := NewPostCloseSweeper(fake, func() []types.Project { return pcProjects("/proj") }, nil, runner, commenter)
	s.scan()

	if got := fake.statuses["demo-re1"]; got != types.StatusOpen {
		t.Fatalf("expected bead reopened to open, got status %q", got)
	}
	labels := fake.labels["demo-re1"]
	if !hasBeadLabel(labels, "postclose-reopened") {
		t.Fatalf("expected postclose-reopened label, got %v", labels)
	}
	if !strings.Contains(commentBody, "FAIL run1") || !strings.Contains(commentBody, "FAIL run2") {
		t.Fatalf("comment must attach BOTH failed-run outputs, got: %q", commentBody)
	}
}

// Flake guard: first run fails, re-run passes → labeled postclose-flake,
// NOT reopened.
func TestPostCloseSweepFlakeLabelsWithoutReopening(t *testing.T) {
	fake := newPCFakeBeads([]types.Bead{
		pcClosedBead("demo-fl1", "go test ./...", time.Hour),
	})
	called := 0
	commenter := func(projectPath string, beadID types.BeadID, body string) error {
		called++
		return nil
	}
	runner := pcRunner(
		struct {
			output string
			err    bool
		}{"FAIL run1", true},
		struct {
			output string
			err    bool
		}{"ok run2", false},
	)
	s := NewPostCloseSweeper(fake, func() []types.Project { return pcProjects("/proj") }, nil, runner, commenter)
	s.scan()

	if got, ok := fake.statuses["demo-fl1"]; ok {
		t.Fatalf("flake must not reopen bead, got status write %q", got)
	}
	labels := fake.labels["demo-fl1"]
	if !hasBeadLabel(labels, "postclose-flake") {
		t.Fatalf("expected postclose-flake label, got %v", labels)
	}
	if called != 0 {
		t.Fatalf("flake path must not attach a comment, got %d", called)
	}
}

// Bounded auto-reopen: bead already carrying postclose-reopened fails again →
// needs-human added, NO second reopen.
func TestPostCloseSweepSecondReopenAddsNeedsHuman(t *testing.T) {
	fake := newPCFakeBeads([]types.Bead{
		pcClosedBead("demo-rb1", "go test ./...", time.Hour, "postclose-reopened"),
	})
	runner := pcRunner(
		struct {
			output string
			err    bool
		}{"FAIL run1", true},
		struct {
			output string
			err    bool
		}{"FAIL run2", true},
	)
	s := NewPostCloseSweeper(fake, func() []types.Project { return pcProjects("/proj") }, nil, runner, nil)
	s.scan()

	if got, ok := fake.statuses["demo-rb1"]; ok {
		t.Fatalf("second failure must not reopen again, got status write %q", got)
	}
	labels := fake.labels["demo-rb1"]
	if !hasBeadLabel(labels, "needs-human") {
		t.Fatalf("expected needs-human label on second failure, got %v", labels)
	}
}

// Skips: beads older than 24h and closed beads without commands.
func TestPostCloseSweepSkipsOldAndCommandlessBeads(t *testing.T) {
	fake := newPCFakeBeads([]types.Bead{
		pcClosedBead("demo-old", "go test ./...", 25*time.Hour),
		pcClosedBead("demo-nocmd", "", time.Hour),
	})
	invoked := 0
	runner := func(projectPath, command string) (string, error) {
		invoked++
		return "", nil
	}
	s := NewPostCloseSweeper(fake, func() []types.Project { return pcProjects("/proj") }, nil, runner, nil)
	s.scan()

	if invoked != 0 {
		t.Fatalf("expected no command invocations, got %d", invoked)
	}
	if len(fake.statuses) != 0 || len(fake.labels) != 0 {
		t.Fatalf("expected no mutations, got statuses=%v labels=%v", fake.statuses, fake.labels)
	}
}

// Integration: closed fixture bead whose command fails in a real default-branch
// git checkout is reopened with the label and comment.
func TestPostCloseSweepIntegrationRealGitRepoReopen(t *testing.T) {
	repo := newPCGitRepo(t)
	fake := newPCFakeBeads([]types.Bead{
		pcClosedBead("demo-int1", "false", time.Hour),
	})
	var commentBody string
	commenter := func(projectPath string, beadID types.BeadID, body string) error {
		commentBody = body
		return nil
	}
	realRunner := func(projectPath, command string) (string, error) {
		cmd := exec.Command("bash", "-c", command)
		cmd.Dir = projectPath
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	s := NewPostCloseSweeper(fake, func() []types.Project { return pcProjects(repo) }, nil, realRunner, commenter)
	s.scan()

	if got := fake.statuses["demo-int1"]; got != types.StatusOpen {
		t.Fatalf("expected fixture bead reopened, got status %q", got)
	}
	if !hasBeadLabel(fake.labels["demo-int1"], "postclose-reopened") {
		t.Fatalf("expected postclose-reopened label, got %v", fake.labels["demo-int1"])
	}
	if !strings.Contains(commentBody, "exit status") {
		t.Fatalf("comment must carry the failing output, got: %q", commentBody)
	}
}

// newPCGitRepo creates a throwaway git repo (default branch) for the
// integration test. Returns its path.
func newPCGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := exec.Command("bash", "-c", "echo hi > fixture.txt && git add fixture.txt && git commit -qm init").Run(); err != nil {
		// cwd differs; redo inside dir
		cmd := exec.Command("bash", "-c", "echo hi > fixture.txt && git add fixture.txt && git commit -qm init")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture commit failed: %v\n%s", err, out)
		}
	}
	return dir
}

// Start/Stop are safe no-ops in the RED stub and must remain safe in GREEN
// (nil-safety contract shared with the other periodic sweepers).
func TestPostCloseSweepStartStopNilSafe(t *testing.T) {
	var s *PostCloseSweeper
	s.Start()
	s.Stop()
	fake := newPCFakeBeads(nil)
	s2 := NewPostCloseSweeper(fake, func() []types.Project { return pcProjects("/proj") }, nil, nil, nil)
	s2.Start()
	s2.Stop()
}

// BenchmarkPostCloseSweepVerifyBead measures the per-bead sweep decision path
// (eligibility check + two passing runCommand invocations) against a fake
// BeadsClient, exercising the sweep's steady-state cost.
func BenchmarkPostCloseSweepVerifyBead(b *testing.B) {
	fake := newPCFakeBeads([]types.Bead{{
		ID:        "pc-bench",
		Title:     "closed bench bead",
		Status:    types.StatusClosed,
		UpdatedAt: time.Now(),
		Metadata:  map[string]string{"test_command": "true"},
	}})
	s := NewPostCloseSweeper(fake,
		func() []types.Project { return pcProjects("/proj") },
		nil,
		func(projectPath, command string) (string, error) { return "ok", nil },
		nil,
	)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.verifyBead(pcProjects("/proj")[0], fake.beads[0])
	}
}
