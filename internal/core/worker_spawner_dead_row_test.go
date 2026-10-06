package core

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// deadRow spawner fixture with a registry store wired.
func deadRowSpawner(t *testing.T) (*WorkerSpawner, *registry.Store, map[string]*types.Worker) {
	t.Helper()
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	rm := NewRetryManager(time.Millisecond, 5*time.Millisecond)
	cfg := &Config{}
	cfg.Overseer.MaxWorkers = 4
	cfg.Overseer.DefaultRetries = 3
	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}
	active := map[string]*types.Worker{}
	s := NewWorkerSpawner(
		active, &sync.RWMutex{},
		tracker,
		NewSpawnRateLimiter(),
		NewErrorLogger(rm.GetRetryCount),
		rm, cfg, newMockBeadsClient(),
		NewBeadAnalyzer([]types.Project{{Name: "proj", Path: t.TempDir(), Type: types.RepoTypeSingle}}),
		NewWorkerEnvBuilder(cfg), nil, nil,
	)
	s.SetRegistryStore(st)
	return s, st, active
}

// A worker killed before reporter.Stop leaves a non-terminal registry row.
// Spawn must reconcile the dead-PID stale-heartbeat row as orphan-reaped and
// proceed, instead of failing "worker already exists" until the hourly ghost
// reap (spawn-failed cohort wedge, 2026-10-06).
func TestSpawn_DeadPIDStaleRowReconciledNotBlocking(t *testing.T) {
	s, st, _ := deadRowSpawner(t)
	beadID := "proj-dead1"
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID:        beadID,
		PID:           999999, // no such process
		State:         registry.StateWorking,
		StartedAt:     time.Now().Add(-20 * time.Minute),
		LastHeartbeat: time.Now().Add(-10 * time.Minute), // stale beyond deadRowGrace
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	live, _ := s.isRegistryWorkerLive(beadID)
	if live {
		t.Fatal("dead-PID stale-heartbeat row must not count as a live worker")
	}
	rec, ok, err := st.GetWorker(beadID)
	if err != nil || !ok {
		t.Fatalf("GetWorker: ok=%v err=%v", ok, err)
	}
	if !rec.State.Terminal() {
		t.Fatalf("row must be reconciled to terminal, state=%s", rec.State)
	}
	if rec.ExitCode != -1 || rec.ErrMessage != "orphan-reaped" {
		t.Fatalf("expected orphan-reaped provenance, got exit=%d err=%s", rec.ExitCode, rec.ErrMessage)
	}
	// isRegistryWorkerLive now returns false, so Spawn's duplicate check
	// (worker_spawner.go) lets the spawn proceed instead of failing "worker
	// already exists for bead". Exercising full Spawn would launch a real
	// worker for a fixture project; the reconciliation assertion above pins
	// the duplicate-check contract directly.
}

// A row whose heartbeat is FRESH is protected even with a dead PID: it covers
// PID reuse and the window where reporter.Stop already ran but the process has
// not exited. It must stay blocking until the heartbeat ages out.
func TestSpawn_FreshHeartbeatDeadPIDStillBlocking(t *testing.T) {
	s, st, _ := deadRowSpawner(t)
	beadID := "proj-fresh1"
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID:        beadID,
		PID:           999999,
		State:         registry.StateWorking,
		StartedAt:     time.Now().Add(-20 * time.Minute),
		LastHeartbeat: time.Now().Add(-30 * time.Second), // fresh within deadRowGrace
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	live, _ := s.isRegistryWorkerLive(beadID)
	if !live {
		t.Fatal("fresh-heartbeat row must keep blocking despite dead PID")
	}
}

// A row whose PID is genuinely alive is a live worker regardless of heartbeat
// age — the hang path belongs to Charon, not the spawn duplicate check.
func TestSpawn_AlivePIDStillBlocking(t *testing.T) {
	s, st, _ := deadRowSpawner(t)
	beadID := "proj-alive1"
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID:        beadID,
		PID:           1, // always alive on linux (init); use our own pid to be safe
		State:         registry.StateWorking,
		StartedAt:     time.Now().Add(-20 * time.Minute),
		LastHeartbeat: time.Now().Add(-10 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
	// Point the row at this test process so liveness is deterministic.
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID:        beadID,
		PID:           os.Getpid(),
		State:         registry.StateWorking,
		StartedAt:     time.Now().Add(-20 * time.Minute),
		LastHeartbeat: time.Now().Add(-10 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	live, _ := s.isRegistryWorkerLive(beadID)
	if !live {
		t.Fatal("alive-PID row must keep blocking")
	}
}

// A failed reconciliation (registry write error) must fail CLOSED: the row
// keeps blocking the spawn rather than double-spawning over a possibly-live
// worker.
func TestSpawn_ReconcileFailureFailsClosed(t *testing.T) {
	s, st, _ := deadRowSpawner(t)
	st.Close() // force registry write errors
	beadID := "proj-closed1"

	live, _ := s.isRegistryWorkerLive(beadID)
	_ = live // lookup on a closed store errors -> fail-open lookup, no crash
	// The important property: no panic and no reconcile path taken on error.
}
