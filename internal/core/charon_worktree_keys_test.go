package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// gybo4 (2026-09-10): the reconciler integrated a live chain worktree because
// the active-workers snapshot is keyed by the member bead actually running
// while worktree branches are keyed by chain root — the skip never matched.
// worktreeActiveKeys must contain both the bead ID and its chain key, plus
// workers the in-memory snapshot lost but the tracker still shows as started
// within the grace window.
func TestWorktreeActiveKeys_ChainKeyMappingAndGraceWindow(t *testing.T) {
	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkerTracker: %v", err)
	}
	if err := tracker.TrackWorkerWithPGID(4321, 4321, "fresh-bead", "/tmp/proj"); err != nil {
		t.Fatalf("TrackWorkerWithPGID: %v", err)
	}

	j := &Charon{workerTracker: tracker}
	j.SetActiveWorkersSnapshot(func() map[string]*types.Worker {
		return map[string]*types.Worker{"member-bead": {}}
	})
	j.SetChainKeyForBeadID(func(id types.BeadID) string {
		if id == "member-bead" {
			return "root-bead"
		}
		return string(id)
	})

	active := j.worktreeActiveKeys()
	for _, key := range []string{"member-bead", "root-bead", "fresh-bead"} {
		if !active[key] {
			t.Fatalf("active[%q] = false, want true (active keys: %v)", key, active)
		}
	}

	// Age the tracked worker past the spawn grace window: it is no longer
	// protected unless the snapshot says so (it doesn't here). GetRunning-
	// WorkerRecords returns copies, so age the stored record in place.
	tracker.mu.Lock()
	tracker.workers[4321].StartedAt = time.Now().Add(-time.Hour)
	tracker.mu.Unlock()
	active = j.worktreeActiveKeys()
	if active["fresh-bead"] {
		t.Fatalf("fresh-bead aged past grace window must not be protected (active: %v)", active)
	}
}
