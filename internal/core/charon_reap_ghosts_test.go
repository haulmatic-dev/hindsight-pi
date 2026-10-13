package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/internal/registry"
)

// Ghost rows: a worker that dies without posting its completion marker
// (SIGKILL, OOM, daemon kill) leaves a working/parked row whose heartbeat
// freezes. Nothing reaps those at runtime — ReapStale ran only at daemon
// startup with a 24h cutoff, so ghosts accumulated all day (observed live:
// 19 stale "working" rows poisoning fleet metrics). Charon's scan cycle now
// reaps heartbeat-stale non-terminal rows hourly. Completed rows are NOT
// touched: they are the completion history behind fleet metrics and belong
// to the startup sweep.
func TestPerformCleanup_ReapsGhostRegistryRows(t *testing.T) {
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkerTracker: %v", err)
	}

	c := NewCharon(beads.NewClient(), tracker, 15*time.Minute)
	c.SetRegistry(st)

	upsertLive(t, st, "b-live", registry.StateWorking, 30*time.Second)   // fresh heartbeat: survives
	upsertLive(t, st, "b-parked", registry.StateParked, 30*time.Second)  // live park heartbeats: survives
	upsertLive(t, st, "b-ghost", registry.StateWorking, 3*time.Hour)     // frozen heartbeat: reaped
	upsertLive(t, st, "b-ghost-park", registry.StateParked, 3*time.Hour) // abandoned park: reaped
	upsertLive(t, st, "b-done", registry.StateCompleted, 3*time.Hour)    // history: survives runtime reap

	c.performCleanup()

	for _, id := range []string{"b-live", "b-parked", "b-done"} {
		if _, ok, _ := st.GetWorker(id); !ok {
			t.Errorf("%s must survive the runtime ghost reap", id)
		}
	}
	for _, id := range []string{"b-ghost", "b-ghost-park"} {
		if _, ok, _ := st.GetWorker(id); ok {
			t.Errorf("%s is a ghost (stale heartbeat, non-terminal) and must be reaped", id)
		}
	}
}
