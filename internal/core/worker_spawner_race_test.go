package core

import (
	"fmt"
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
// the in-flight placeholder for an orphan, deletes it, and proceeds -- two
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
		tracker, // tracker set, bead absent -> the orphan-delete branch
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
		t.Fatal("placeholder must survive -- it is an in-flight spawn, not an orphan")
	}
}

// If Spawn fails after registering its {status:"spawning"} placeholder,
// the placeholder must be removed so it does not consume a worker slot forever.
func TestSpawn_FailureRemovesPlaceholder(t *testing.T) {
	rm := NewRetryManager(time.Millisecond, 5*time.Millisecond)
	cfg := &Config{}
	cfg.Overseer.MaxWorkers = 4
	cfg.Overseer.DefaultRetries = 2
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
	// Exhaust the retry budget before spawning so that the post-placeholder
	// retry gate returns an error and exercises the deferred cleanup path.
	beadID := "proj-fail1"
	rm.RecordRetry(beadID, fmt.Errorf("boom"))
	rm.RecordRetry(beadID, fmt.Errorf("boom"))
	bead := types.Bead{ID: types.BeadID(beadID), Project: "proj", Type: "bug"}

	if err := s.Spawn(bead); err == nil {
		t.Fatalf("expected spawn failure after retry exhaustion, got nil")
	}
	if _, ok := active[beadID]; ok {
		t.Fatal("placeholder must be removed after spawn failure")
	}
}

// A placeholder older than 10 minutes with no live process is dead by
// definition and must be reaped by Charon.
func TestSpawn_StalePlaceholderReaped(t *testing.T) {
	j := &Charon{}
	var cleared []string
	old := time.Now().Add(-11 * time.Minute)
	j.SetActiveWorkersSnapshot(func() map[string]*types.Worker {
		return map[string]*types.Worker{
			"proj-stale": {ID: "proj-stale", PID: 0, Status: "spawning", StartTime: old},
		}
	})
	j.SetOnStaleActiveWorker(func(beadID string) { cleared = append(cleared, beadID) })

	if got := j.clearStaleActiveWorkers(); got != 1 {
		t.Fatalf("expected 1 stale placeholder reaped, got %d", got)
	}
	if len(cleared) != 1 || cleared[0] != "proj-stale" {
		t.Fatalf("expected callback for proj-stale, got %v", cleared)
	}
}

// A placeholder younger than 10 minutes must never be reaped, even if its
// PID is 0. This preserves the PR #163 double-spawn guard.
func TestSpawn_YoungPlaceholderNotReaped(t *testing.T) {
	j := &Charon{}
	var cleared []string
	j.SetActiveWorkersSnapshot(func() map[string]*types.Worker {
		return map[string]*types.Worker{
			"proj-fresh": {ID: "proj-fresh", PID: 0, Status: "spawning", StartTime: time.Now().Add(-2 * time.Minute)},
		}
	})
	j.SetOnStaleActiveWorker(func(beadID string) { cleared = append(cleared, beadID) })

	if got := j.clearStaleActiveWorkers(); got != 0 {
		t.Fatalf("expected 0 young placeholders reaped, got %d", got)
	}
	if len(cleared) != 0 {
		t.Fatalf("expected no callback, got %v", cleared)
	}
}
