package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestDetectStrandedWork_SkipsLiveWorker(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	key := "srv-live"
	makeStrandedBranch(t, projectPath, key)

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusCompleted, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	units := DetectStrandedWork(nil, projectPath, map[string]bool{key: true}, nil, beads, sink)
	if len(units) != 0 {
		t.Fatalf("live key must not be stranded, got %v", units)
	}
	if len(readEventsOfType(t, sink, "worktree_stranded")) != 0 {
		t.Fatal("no event expected for live key")
	}
}

func TestDetectStrandedWork_SkipsActiveChain(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	key := "srv-chain"
	makeStrandedBranch(t, projectPath, key)

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusCompleted, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	detector := NewChainCompletionDetector(ChainCompletionConfig{}, &Overseer{dag: NewDAG()})
	detector.RegisterChain(types.BeadID(key), []types.BeadID{types.BeadID(key)}, projectPath)

	units := DetectStrandedWork(nil, projectPath, nil, detector, beads, sink)
	if len(units) != 0 {
		t.Fatalf("active chain must not be stranded, got %v", units)
	}
}

func TestDetectStrandedWork_SkipsEmptyDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	key := "srv-empty"
	// Branch exists but has no commits ahead of main.
	runGitCmd(t, projectPath, "checkout", "-b", "hydra/wt-"+key)
	runGitCmd(t, projectPath, "checkout", "-")

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusCompleted, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	units := DetectStrandedWork(nil, projectPath, nil, nil, beads, sink)
	if len(units) != 0 {
		t.Fatalf("empty diff must not be stranded, got %v", units)
	}
}

func TestDetectStrandedWork_SkipsOpenBead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	key := "srv-open"
	makeStrandedBranch(t, projectPath, key)

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusOpen, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	units := DetectStrandedWork(nil, projectPath, nil, nil, beads, sink)
	if len(units) != 0 {
		t.Fatalf("open bead must not be stranded, got %v", units)
	}
}

func TestDetectStrandedWork_DetectsTerminalBead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	key := "srv-terminal"
	makeStrandedBranch(t, projectPath, key)

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusCompleted, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	units := DetectStrandedWork(nil, projectPath, nil, nil, beads, sink)
	if len(units) != 1 {
		t.Fatalf("detected = %d, want 1", len(units))
	}
	if units[0].Key != key {
		t.Fatalf("key = %q, want %q", units[0].Key, key)
	}

	ev := readEventsOfType(t, sink, "worktree_stranded")
	if len(ev) != 1 {
		t.Fatalf("worktree_stranded events = %d, want 1", len(ev))
	}
	got := beads.beads[key]
	if !hasLabel(got.Labels, strandedWorkLabel) {
		t.Fatalf("bead labels %v missing %q", got.Labels, strandedWorkLabel)
	}
}

func TestDetectStrandedWork_DedupByLabel(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	key := "srv-dup"
	makeStrandedBranch(t, projectPath, key)

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusCompleted, Labels: []string{strandedWorkLabel}, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	units := DetectStrandedWork(nil, projectPath, nil, nil, beads, sink)
	if len(units) != 0 {
		t.Fatalf("already-labelled bead must not be re-reported, got %v", units)
	}
}

func TestDetectStrandedWork_VersionedKeyResolvesToBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	key := "srv-version"
	makeStrandedBranch(t, projectPath, key)
	runGitCmd(t, projectPath, "branch", "-m", "hydra/wt-"+key, "hydra/wt-"+key+"-v2")

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusCompleted, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	units := DetectStrandedWork(nil, projectPath, nil, nil, beads, sink)
	if len(units) != 1 || units[0].Key != key {
		t.Fatalf("versioned key did not resolve to base: got %v", units)
	}
}

func TestIntegrateStrandedRepair_Success(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "srv-integ"

	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, key+".txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wt, "bead("+key+"): add file"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusCompleted, Labels: []string{strandedWorkLabel}, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	if err := IntegrateStrandedRepair(projectPath, key, m, beads, sink); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if branchExists(projectPath, m.branchName(key)) {
		t.Fatal("branch should be removed after successful integration")
	}
	if _, err := os.Stat(filepath.Join(projectPath, key+".txt")); err != nil {
		t.Fatalf("file should be on working branch: %v", err)
	}
	if len(readEventsOfType(t, sink, "stranded_integrated")) != 1 {
		t.Fatal("expected one stranded_integrated event")
	}
	if hasLabel(beads.beads[key].Labels, strandedWorkLabel) {
		t.Fatal("stranded-work label should be removed on success")
	}
}

func TestIntegrateStrandedRepair_Conflict(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "srv-conflict"

	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "shared.txt"), []byte("main content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, projectPath, "add", "shared.txt")
	runGitCmd(t, projectPath, "commit", "-m", "main shared")

	if err := os.WriteFile(filepath.Join(wt, "shared.txt"), []byte("worktree content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wt, "bead("+key+"): add shared file"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusCompleted, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	if err := IntegrateStrandedRepair(projectPath, key, m, beads, sink); err != nil {
		t.Fatalf("conflict repair must return nil, got %v", err)
	}
	if branchExists(projectPath, m.branchName(key)) {
		t.Fatal("conflicting branch should be consumed")
	}
	if !branchExists(projectPath, "hydra/quarantine/"+key) {
		t.Fatal("conflicting work should be quarantined")
	}
	if !hasLabel(beads.beads[key].Labels, "stranded-conflict") {
		t.Fatalf("bead labels %v missing stranded-conflict", beads.beads[key].Labels)
	}
	if len(readEventsOfType(t, sink, "stranded_conflict")) != 1 {
		t.Fatal("expected one stranded_conflict event")
	}
	got, _ := os.ReadFile(filepath.Join(projectPath, "shared.txt"))
	if string(got) != "main content\n" {
		t.Fatalf("working branch shared.txt = %q, want main content", got)
	}
}

type fakeStrandedManager struct {
	resolve        string
	integrateErr   error
	quarantine     string
	quarantineErr  error
}

func (f *fakeStrandedManager) IntegrateBranchAndRemove(_, _, _ string) error { return f.integrateErr }
func (f *fakeStrandedManager) QuarantineBranchAndRemove(_, _, _ string) (string, error) { return f.quarantine, f.quarantineErr }
func (f *fakeStrandedManager) ResolveBranch(_, _ string) string              { return f.resolve }

func TestIntegrateStrandedRepair_VerificationFailure(t *testing.T) {
	projectPath := gitInitRepo(t)
	key := "srv-unverified"
	m := &fakeStrandedManager{
		resolve:      "hydra/wt-" + key,
		integrateErr: errors.New("integrate hydra/wt-srv-unverified: merge verification failed: branch tip abc not contained in HEAD def"),
	}
	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{ID: types.BeadID(key), Status: types.StatusCompleted, Project: filepath.Base(projectPath)}
	sink := NewEventSink(t.TempDir())

	err := IntegrateStrandedRepair(projectPath, key, m, beads, sink)
	if err == nil {
		t.Fatal("verification failure must return an error")
	}
	if !hasLabel(beads.beads[key].Labels, "integration-unverified") {
		t.Fatalf("bead labels %v missing integration-unverified", beads.beads[key].Labels)
	}
	ev := readEventsOfType(t, sink, "stranded_integrated")
	if len(ev) != 1 || ev[0].Reason != "verification_failed" {
		t.Fatalf("expected one verification_failed event, got %+v", ev)
	}
}

func makeStrandedBranch(t *testing.T, projectPath, key string) {
	t.Helper()
	runGitCmd(t, projectPath, "checkout", "-b", "hydra/wt-"+key)
	if err := os.WriteFile(filepath.Join(projectPath, key+".txt"), []byte(key+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, projectPath, "add", key+".txt")
	runGitCmd(t, projectPath, "commit", "-m", "bead("+key+"): add file")
	runGitCmd(t, projectPath, "checkout", "-")
}

// Ensure the test compiles even if no other file uses the production WorktreeManager parameter.
var _ = NewWorktreeManager
var _ = time.Now

// FR-021: an open bead quarantined by integrate-stranded (or marked
// integration-unverified) is no longer resumable by a worker — the detector
// must accept it instead of skipping the "still live / resumable" check.
func TestDetectStrandedWork_AcceptsOpenQuarantineLabeledBead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	key := "srv-quarantined"
	makeStrandedBranch(t, projectPath, key)

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{
		ID: types.BeadID(key), Status: types.StatusOpen,
		Labels: []string{strandedConflictLabel},
		Project: filepath.Base(projectPath),
	}
	sink := NewEventSink(t.TempDir())

	units := DetectStrandedWork(nil, projectPath, nil, nil, beads, sink)
	if len(units) != 1 {
		t.Fatalf("open quarantine-labeled bead must be stranded, got %d units", len(units))
	}
	if !hasLabel(beads.beads[key].Labels, strandedWorkLabel) {
		t.Fatalf("bead labels %v missing %q", beads.beads[key].Labels, strandedWorkLabel)
	}
	// Dedup: second sweep must not re-report.
	if again := DetectStrandedWork(nil, projectPath, nil, nil, beads, sink); len(again) != 0 {
		t.Fatalf("second sweep re-reported %d units", len(again))
	}
}

func TestDetectStrandedWork_AcceptsOpenIntegrationUnverifiedBead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	key := "srv-unverified"
	makeStrandedBranch(t, projectPath, key)

	beads := newMockBeadsClient()
	beads.beads[key] = types.Bead{
		ID: types.BeadID(key), Status: types.StatusOpen,
		Labels: []string{integrationUnverifiedLabel},
		Project: filepath.Base(projectPath),
	}

	units := DetectStrandedWork(nil, projectPath, nil, nil, beads, nil)
	if len(units) != 1 {
		t.Fatalf("open integration-unverified bead must be stranded, got %d units", len(units))
	}
}
