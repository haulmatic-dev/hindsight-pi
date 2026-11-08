package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Reproduction probe for the reported wedge (2026-10-13,
// ajantha-srv-smart-engine-mqn): a transient spawn failure (br show exit 8
// while the store was mid-write) must not leave a stale in-memory
// activeWorkers entry behind, and the bead must be immediately re-spawnable.
//
// Incident-simulating sequence, all inside WorkerSpawner.Spawn (the single
// choke point the overseer preflights funnel into):
//  1. attempt 1: a transient infra failure AFTER the spawning placeholder
//     claims the slot (fork/exec EACCES on a non-executable stub binary —
//     same class as the fork/exec ENOENT burst in the incident) → Spawn errors;
//  2. assert NO activeWorkers entry survives the failure (rollback) and the
//     failure is re-spawnable (no permanent "worker already exists" block).
func TestSpawn_TransientFailureLeavesNoStaleClaim(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "worker")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntrue\n"), 0o600); err != nil {
		t.Fatalf("write stub binary: %v", err)
	}
	t.Setenv("HYDRA_WORKER_BINARY", bin) // present but NOT executable → cmd.Start fails

	rm := NewRetryManager(time.Millisecond, 5*time.Millisecond)
	cfg := &Config{}
	cfg.Overseer.MaxWorkers = 4
	cfg.Overseer.DefaultRetries = 5
	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}
	mock := &labelRecordingBeadsClient{} // GetBead returns nil,nil (store reachable)
	projectDir := t.TempDir()
	s := NewWorkerSpawner(
		map[string]*types.Worker{}, &sync.RWMutex{},
		tracker,
		NewSpawnRateLimiter(),
		NewErrorLogger(rm.GetRetryCount),
		rm, cfg, mock,
		NewBeadAnalyzer([]types.Project{{Name: "proj", Path: projectDir, Type: types.RepoTypeSingle}}),
		NewWorkerEnvBuilder(cfg), nil, nil,
	)

	bead := types.Bead{ID: "proj-stale1", Project: "proj", Type: "bug"}

	for attempt := 1; attempt <= 2; attempt++ {
		// Clear the deterministic rate-limiter backoff the transient fork
		// failure legitimately records — the point of the test is the stale
		// claim, not the backoff policy.
		s.spawnRateLimiter.ClearFailures()
		err := s.Spawn(bead)
		if err == nil {
			t.Fatalf("attempt %d must fail (binary not executable)", attempt)
		}
		if strings.Contains(err.Error(), "worker already exists") {
			t.Fatalf("attempt %d blocked by a stale claim instead of retrying: %v", attempt, err)
		}
		s.workerMutex.RLock()
		n := len(s.activeWorkers)
		s.workerMutex.RUnlock()
		if n != 0 {
			t.Fatalf("attempt %d: stale activeWorkers entry survived transient spawn failure (n=%d) — permanent silent skip", attempt, n)
		}
	}
}

// isBeadBlockingSpawn was the hypothesized mechanism in the bug report
// (in-memory claim added before the br-show gate and never rolled back). It
// does not exist on this base; this test documents the actual contract that
// replaces it: an entry left behind by a half-completed spawn is
// self-healing — the next Spawn attempt's duplicate check cleans up a worker
// that is not in the tracker instead of failing forever with
// "worker already exists".
func TestSpawn_StaleClaimWithoutTrackerRowSelfHeals(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "worker")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntrue\n"), 0o600); err != nil {
		t.Fatalf("write stub binary: %v", err)
	}
	t.Setenv("HYDRA_WORKER_BINARY", bin) // never reaches process start

	rm := NewRetryManager(time.Millisecond, 5*time.Millisecond)
	cfg := &Config{}
	cfg.Overseer.MaxWorkers = 4
	cfg.Overseer.DefaultRetries = 5
	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}
	mock := &labelRecordingBeadsClient{}
	projectDir := t.TempDir()
	s := NewWorkerSpawner(
		map[string]*types.Worker{}, &sync.RWMutex{},
		tracker,
		NewSpawnRateLimiter(),
		NewErrorLogger(rm.GetRetryCount),
		rm, cfg, mock,
		NewBeadAnalyzer([]types.Project{{Name: "proj", Path: projectDir, Type: types.RepoTypeSingle}}),
		NewWorkerEnvBuilder(cfg), nil, nil,
	)

	bead := types.Bead{ID: "proj-stale2", Project: "proj", Type: "bug"}

	// Inject the phantom: a real-looking worker entry with no tracker row.
	s.activeWorkers[bead.ID.String()] = &types.Worker{ID: bead.ID.String(), BeadID: bead.ID, Status: "running"}

	s.spawnRateLimiter.ClearFailures()
	err = s.Spawn(bead)
	if err == nil {
		t.Fatal("spawn must fail with the injected transient condition (binary not executable)")
	}
	if strings.Contains(err.Error(), "worker already exists") {
		t.Fatalf("stale claim without a tracker row permanently blocked the spawn: %v", err)
	}
	s.workerMutex.RLock()
	_, stillThere := s.activeWorkers[bead.ID.String()]
	s.workerMutex.RUnlock()
	if stillThere {
		t.Fatal("stale claim was not cleaned up by the duplicate check")
	}
}
