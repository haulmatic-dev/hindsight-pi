package core

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
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

// A parked worker row in the registry must block respawn: the first worker is
// still alive and may resume. This was the fifth resurrection vector: worker A
// parked at 08:46 and the scheduler spawned worker B at 09:36 because the
// duplicate check ignored the parked row.
func TestSpawn_ParkedRegistryRowBlocksRespawn(t *testing.T) {
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	defer st.Close()

	beadID := "proj-park1"
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID:        beadID,
		PID:           1234,
		State:         registry.StateParked,
		StartedAt:     time.Now().Add(-5 * time.Minute),
		LastHeartbeat: time.Now().Add(-30 * time.Second),
		ParkSince:     time.Now().Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

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

	bead := types.Bead{ID: types.BeadID(beadID), Project: "proj", Type: "bug"}
	err = s.Spawn(bead)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("spawn must refuse a bead with a parked registry row, got: %v", err)
	}
}

// A parked worker row that is older than the respawn timeout is stale and may
// be respawned. A live-but-old park should still block until Charon reaps it,
// but the timeout prevents parked ghosts from blocking a bead forever.
func TestSpawn_StaleParkedRowAllowsRespawn(t *testing.T) {
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	defer st.Close()

	beadID := "proj-park-stale"
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID:        beadID,
		PID:           1234,
		State:         registry.StateParked,
		StartedAt:     time.Now().Add(-30 * time.Minute),
		LastHeartbeat: time.Now().Add(-10 * time.Minute),
		ParkSince:     time.Now().Add(-20 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

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
	// Shrink the timeout so the stale ParkSince above exceeds it.
	s.parkedRespawnTimeout = 5 * time.Minute

	live, err := s.isRegistryWorkerLive(beadID)
	if err != nil {
		t.Fatalf("isRegistryWorkerLive: %v", err)
	}
	if live {
		t.Fatalf("stale parked row (park age > timeout) must not block respawn")
	}

	// The duplicate-spawn guard has already cleared the stale row, so the
	// spawner would proceed to process creation. We do not run the full Spawn
	// here because the test environment lacks a worker binary; the live-row
	// check is the behavior under test.
}

// Non-parked live rows must also be counted as existing workers.
func TestSpawn_WorkingRegistryRowBlocksRespawn(t *testing.T) {
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	defer st.Close()

	beadID := "proj-working"
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID:        beadID,
		PID:           1234,
		State:         registry.StateWorking,
		StartedAt:     time.Now().Add(-5 * time.Minute),
		LastHeartbeat: time.Now().Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

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

	bead := types.Bead{ID: types.BeadID(beadID), Project: "proj", Type: "bug"}
	err = s.Spawn(bead)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("spawn must refuse a bead with a working registry row, got: %v", err)
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
