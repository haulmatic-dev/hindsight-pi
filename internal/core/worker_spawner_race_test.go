package core

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Double-spawn hole (j81fp spawned twice in the same second, 2026-08-21):
// Spawn inserts a {status: "spawning"} placeholder into activeWorkers BEFORE
// the process exists in the worker tracker. A concurrent Spawn for the same
// bead finds the placeholder, consults the tracker, finds nothing, mistakes
// the in-flight placeholder for an orphan, deletes it, and proceeds — two
// workers for one bead. A "spawning" placeholder is proof of life, not an
// orphan.
func TestSpawn_SpawningPlaceholderIsNotOrphan(t *testing.T) {
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
		tracker, // tracker set, bead absent → the orphan-delete branch
		NewSpawnRateLimiter(),
		NewErrorLogger(rm.GetRetryCount),
		rm, cfg, newMockBeadsClient(),
		NewBeadAnalyzer([]types.Project{{Name: "proj", Path: t.TempDir(), Type: types.RepoTypeSingle}}),
		NewWorkerEnvBuilder(cfg), nil, nil,
	)
	bead := types.Bead{ID: "proj-race1", Project: "proj", Type: "bug"}

	// simulate an in-flight spawn: placeholder present, tracker not yet written
	active["proj-race1"] = &types.Worker{ID: "proj-race1", Status: "spawning"}

	err = s.Spawn(bead)
	if err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("second spawn must be rejected as duplicate, got %v", err)
	}
	if _, ok := active["proj-race1"]; !ok {
		t.Fatal("placeholder must survive — it is an in-flight spawn, not an orphan")
	}
}
