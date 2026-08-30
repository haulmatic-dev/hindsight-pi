package core

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Regression (2026-08-30, srv-smart-engine-v4bpl): a spawn-failure record from
// a superseded attempt persisted after a later spawn SUCCEEDED, keeping the
// bead in Pan's BlockedBeads candidacy; Pan then abandoned the healthy
// 2-minute-old worker. A successful spawn must clear the stale failure record.
func TestSpawn_SuccessClearsSpawnFailure(t *testing.T) {
	rm := NewRetryManager(time.Millisecond, 5*time.Millisecond)
	cfg := &Config{}
	cfg.Overseer.MaxWorkers = 2
	cfg.Overseer.DefaultRetries = 3
	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}
	projDir := t.TempDir()
	active := map[string]*types.Worker{}
	// The Monitor goroutine's failure path (Wait -> HandleFailure) dereferences
	// the DAG, so it must be real even though the bead is never added to it.
	lifecycle := NewWorkerLifecycle(rm, NewErrorLogger(rm.GetRetryCount), tracker,
		nil, NewDAG(), &sync.RWMutex{}, map[string]bool{},
		func(string) {}, func(*types.Worker, bool, int) error { return nil },
		func() {}, func(*types.Worker, bool, int, time.Duration) {}, func() int { return 3 }, nil)
	s := NewWorkerSpawner(
		active, &sync.RWMutex{}, tracker, NewSpawnRateLimiter(),
		NewErrorLogger(rm.GetRetryCount), rm, cfg, newMockBeadsClient(),
		NewBeadAnalyzer([]types.Project{{Name: "proj", Path: projDir, Type: types.RepoTypeSingle}}),
		NewWorkerEnvBuilder(cfg), lifecycle, nil,
	)

	// The stub exits fast on purpose: the Monitor goroutine then drives the
	// real Wait -> HandleFailure path (with the DAG above) before the test ends.
	fakeWorker := filepath.Join(t.TempDir(), "fake-worker")
	if err := os.WriteFile(fakeWorker, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYDRA_WORKER_BINARY", fakeWorker)

	bead := types.Bead{ID: "proj-clear1", Project: "proj", Type: "bug", Title: "t"}

	// Simulate the earlier failed attempt leaving a record behind.
	s.recordSpawnFailure(bead.ID.String(), errFake)
	if got := s.ListSpawnFailures(); len(got) != 1 {
		t.Fatalf("precondition: 1 recorded failure, got %v", got)
	}

	if err := s.Spawn(bead); err != nil {
		t.Fatalf("spawn with stub binary should succeed: %v", err)
	}
	if got := s.ListSpawnFailures(); len(got) != 0 {
		t.Fatalf("successful spawn must clear the stale spawn-failure record, still: %v", got)
	}
}

var errFake = &fakeErr{"worker already exists for bead proj-clear1"}

type fakeErr struct{ s string }

func (e *fakeErr) Error() string { return e.s }
