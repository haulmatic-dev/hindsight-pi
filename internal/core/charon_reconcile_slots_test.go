package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Reproduction of the 2026-09-11 12:00 phantom-slot wedge: the tracker and
// the in-memory activeWorkers map both hold workers whose processes are long
// dead and whose registry rows are gone ("tracked=2 running" / "slots filled
// 2/2" with 0 live processes and an empty registry). The performCleanup tick
// must reconcile the in-memory set against live PIDs and free the slots;
// before the fix the no-tracked-workers early return skipped both in-memory
// reconcilers and the pool stayed wedged until daemon restart.
func TestCharon_TickFreesSlotsWhenAllTrackedWorkersDead(t *testing.T) {
	dir := t.TempDir()
	wt := writePlaceholderWorkers(t, dir, 11*time.Minute, 2)

	// In-memory activeWorkers mirror: same beads, real (dead) PIDs.
	snapshot := map[string]*types.Worker{}
	for pid, rec := range wt.GetRunningWorkerRecords() {
		snapshot[rec.BeadID] = &types.Worker{
			ID:        rec.BeadID,
			PID:       pid,
			Status:    "running",
			StartTime: rec.StartedAt,
		}
	}
	if len(snapshot) != 2 {
		t.Fatalf("setup: expected 2 snapshot entries, got %d", len(snapshot))
	}

	j := NewCharon(nil, wt, 0)
	j.SetMaxWorkers(2)
	j.orphanBeadGrace = 0 // no beads client in this unit test
	j.SetActiveWorkersSnapshot(func() map[string]*types.Worker { return snapshot })
	var cleared []string
	j.SetOnStaleActiveWorker(func(beadID string) {
		cleared = append(cleared, beadID)
		delete(snapshot, beadID)
	})

	j.performCleanup()

	if len(snapshot) != 0 {
		t.Fatalf("phantom slots wedged: %d active workers still hold slots after a Charon tick (cleared=%v)",
			len(snapshot), cleared)
	}
}

// A dead in-memory PID must NOT be reaped when the registry still shows a
// fresh heartbeat for the bead: the heartbeat is the liveness truth (the
// worker may have been re-exec'd or the PID view may be stale), and reaping
// would double-spawn a live worker.
func TestCharon_ReconcileKeepsSlotWithFreshRegistryHeartbeat(t *testing.T) {
	dir := t.TempDir()
	wt := writePlaceholderWorkers(t, dir, 11*time.Minute, 1)
	var deadPID int
	var beadID string
	for pid, rec := range wt.GetRunningWorkerRecords() {
		deadPID, beadID = pid, rec.BeadID
	}

	snapshot := map[string]*types.Worker{
		beadID: {ID: beadID, PID: deadPID, Status: "running", StartTime: time.Now().Add(-11 * time.Minute)},
	}

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID:        beadID,
		PID:           deadPID,
		State:         registry.StateWorking,
		StartedAt:     time.Now().Add(-11 * time.Minute),
		LastHeartbeat: time.Now().Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	j := NewCharon(nil, wt, 0)
	j.SetMaxWorkers(1)
	j.orphanBeadGrace = 0
	j.SetRegistry(st)
	j.SetActiveWorkersSnapshot(func() map[string]*types.Worker { return snapshot })
	j.SetOnStaleActiveWorker(func(id string) { delete(snapshot, id) })

	j.performCleanup()

	if len(snapshot) != 1 {
		t.Fatalf("slot reaped despite fresh registry heartbeat — live worker would be double-spawned")
	}
}
