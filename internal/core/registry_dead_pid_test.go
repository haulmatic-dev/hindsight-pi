package core

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Regression (2026-08-31): daemon restart killed workers but left their
// registry rows behind. A dead-PID row blocked respawn; reconciling it at
// startup must mark the row completed so the bead can respawn.
func TestRegistryDeadPIDReconcileUnblocksSpawn(t *testing.T) {
	reg, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry open: %v", err)
	}
	defer reg.Close()

	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}

	spawner := NewWorkerSpawner(
		map[string]*types.Worker{}, &sync.RWMutex{}, tracker,
		NewSpawnRateLimiter(), NewErrorLogger(func(string) int { return 0 }),
		NewRetryManager(time.Millisecond, 5*time.Millisecond), &Config{},
		nil, nil, nil, nil, nil,
	)
	spawner.SetRegistryStore(reg)

	dead := registry.WorkerRecord{
		BeadID:        "bead-dead",
		PID:           999999,
		PGID:          999999,
		Project:       "test",
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		State:         registry.StateWorking,
	}
	if err := reg.UpsertWorker(dead); err != nil {
		t.Fatalf("upsert dead: %v", err)
	}

	if !spawner.HasLiveRegistryWorker("bead-dead") {
		t.Fatal("precondition: dead-PID row should look live before reconcile")
	}

	n, ids, err := reg.ReconcileDeadPIDs()
	if err != nil {
		t.Fatalf("ReconcileDeadPIDs: %v", err)
	}
	if n != 1 || len(ids) != 1 || ids[0] != "bead-dead" {
		t.Fatalf("expected 1 dead row, got n=%d ids=%v", n, ids)
	}

	if spawner.HasLiveRegistryWorker("bead-dead") {
		t.Fatalf("dead-PID row still blocks spawn after reconcile")
	}

	live := registry.WorkerRecord{
		BeadID:        "bead-live",
		PID:           os.Getpid(),
		PGID:          os.Getpid(),
		Project:       "test",
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		State:         registry.StateParked,
	}
	if err := reg.UpsertWorker(live); err != nil {
		t.Fatalf("upsert live: %v", err)
	}

	n, _, err = reg.ReconcileDeadPIDs()
	if err != nil {
		t.Fatalf("ReconcileDeadPIDs 2: %v", err)
	}
	if n != 0 {
		t.Fatalf("live row was reaped unexpectedly")
	}
	if !spawner.HasLiveRegistryWorker("bead-live") {
		t.Fatalf("live parked row no longer blocks spawn")
	}
}
