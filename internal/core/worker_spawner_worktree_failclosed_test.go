package core

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// labelRecordingBeadsClient records UpdateBeadLabels calls; every other
// BeadsClient method panics if called (embed nil interface).
type labelRecordingBeadsClient struct {
	BeadsClient
	labels []string
	calls  int
}

func (m *labelRecordingBeadsClient) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	m.calls++
	m.labels = append([]string(nil), labels...)
	return nil
}

func (m *labelRecordingBeadsClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	return nil, nil
}

func (m *labelRecordingBeadsClient) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	return nil
}

// Shared-checkout fallback was a corruption engine (2026-09-11/12): workers
// that lost their worktree ran loose in the user's main checkout, committing
// merge-conflict markers into .beads/issues.jsonl, wiping .beads via
// git clean -fdx, and landing janitorial commits on the user's branch. The
// spawn must fail CLOSED — never downgrade isolation — and hard-park the bead
// with the durable worktree-unavailable label.
func TestSpawnInternal_WorktreeFailureFailsClosedAndHardParks(t *testing.T) {
	rm := NewRetryManager(time.Millisecond, 5*time.Millisecond)
	cfg := &Config{}
	cfg.Overseer.MaxWorkers = 4
	cfg.Overseer.DefaultRetries = 2
	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}
	mock := &labelRecordingBeadsClient{}
	projectDir := t.TempDir() // not a git repo: EnsureWorktree cannot succeed
	s := NewWorkerSpawner(
		map[string]*types.Worker{}, &sync.RWMutex{},
		tracker,
		NewSpawnRateLimiter(),
		NewErrorLogger(rm.GetRetryCount),
		rm, cfg, mock,
		NewBeadAnalyzer([]types.Project{{Name: "proj", Path: projectDir, Type: types.RepoTypeSingle}}),
		NewWorkerEnvBuilder(cfg), nil, nil,
	)
	s.SetWorktreeManager(NewWorktreeManager(), func(types.Bead) string { return "chain1" })

	bead := types.Bead{ID: "proj-wtfc1", Project: "proj", Type: "bug"}
	_, err = s.SpawnInternal(bead)
	if err == nil {
		t.Fatal("spawn must fail closed when worktree setup fails")
	}
	if !strings.Contains(err.Error(), "worktree-unavailable") || !strings.Contains(err.Error(), "hard-parked") {
		t.Fatalf("error must name the hard-park reason, got: %v", err)
	}
	if mock.calls != 1 || len(mock.labels) != 1 || mock.labels[0] != worktreeUnavailableLabel {
		t.Fatalf("bead must be labelled %q exactly once, got calls=%d labels=%v", worktreeUnavailableLabel, mock.calls, mock.labels)
	}
	if n := len(s.activeWorkers); n != 0 {
		t.Fatalf("no worker may be registered on a fail-closed spawn, got %d", n)
	}
}

// The hard-park label must survive respawn paths: a labelled bead is skipped
// by the DAG ready filter (mirrors the escalatedLabel contract).
func TestGetReadyBeads_SkipsWorktreeUnavailableLabel(t *testing.T) {
	dag := NewDAG()
	a := types.Bead{ID: "a", Type: "bug", Status: types.StatusOpen}
	b := types.Bead{ID: "b", Type: "bug", Status: types.StatusOpen, Labels: []string{worktreeUnavailableLabel}}
	if err := dag.AddNode(a); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := dag.AddNode(b); err != nil {
		t.Fatalf("add b: %v", err)
	}
	n := NewDAGNavigator(dag)
	ready := n.GetReadyBeads(nil)
	if len(ready) != 1 || ready[0].ID != "a" {
		t.Fatalf("worktree-unavailable bead must be filtered from ready set, got %v", ready)
	}
}
