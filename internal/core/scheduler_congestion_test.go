package core

import (
	"errors"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestScheduler_ShouldPauseForCongestion_TriggersAndHolds(t *testing.T) {
	callCount := 0
	s := &Scheduler{
		congestionCounter: func() (int, error) {
			callCount++
			return 4, nil
		},
	}

	paused, err := s.shouldPauseForCongestion()
	if err != nil {
		t.Fatalf("shouldPauseForCongestion: %v", err)
	}
	if !paused {
		t.Fatal("expected congestion pause with 4 parked workers")
	}
	if callCount != 1 {
		t.Errorf("expected counter called once, got %d", callCount)
	}
	if s.congestionPausedUntil.IsZero() {
		t.Fatal("expected congestionPausedUntil set")
	}

	// While the pause is active, the counter should not be consulted again.
	paused, err = s.shouldPauseForCongestion()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !paused {
		t.Fatal("expected pause to be held")
	}
	if callCount != 1 {
		t.Errorf("expected counter still called once, got %d", callCount)
	}
}

func TestScheduler_ShouldPauseForCongestion_Recovers(t *testing.T) {
	s := &Scheduler{
		congestionCounter: func() (int, error) { return 1, nil },
	}
	// Manually set an expired pause.
	s.congestionPausedUntil = time.Now().Add(-time.Minute)

	paused, err := s.shouldPauseForCongestion()
	if err != nil {
		t.Fatalf("shouldPauseForCongestion: %v", err)
	}
	if paused {
		t.Fatal("expected pause to be cleared after expiry")
	}
	if !s.congestionPausedUntil.IsZero() {
		t.Errorf("expected congestionPausedUntil cleared, got %v", s.congestionPausedUntil)
	}
}

func TestScheduler_ShouldPauseForCongestion_FailOpen(t *testing.T) {
	s := &Scheduler{}
	paused, err := s.shouldPauseForCongestion()
	if err != nil {
		t.Fatalf("shouldPauseForCongestion: %v", err)
	}
	if paused {
		t.Fatal("expected no pause when congestion counter is absent")
	}
}

func TestScheduler_ShouldPauseForCongestion_CounterError(t *testing.T) {
	s := &Scheduler{
		congestionCounter: func() (int, error) { return 0, errors.New("registry down") },
	}
	paused, err := s.shouldPauseForCongestion()
	if err != nil {
		t.Fatalf("shouldPauseForCongestion: %v", err)
	}
	if paused {
		t.Fatal("expected fail-open on counter error")
	}
}

func TestScheduler_ShouldPauseForCongestion_AtThreshold(t *testing.T) {
	s := &Scheduler{
		congestionCounter: func() (int, error) { return 3, nil },
	}
	paused, err := s.shouldPauseForCongestion()
	if err != nil {
		t.Fatalf("shouldPauseForCongestion: %v", err)
	}
	if !paused {
		t.Fatal("expected pause at threshold")
	}
}

func TestScheduler_ShouldSpawnWorker_ParkedRegistryRowBlocks(t *testing.T) {
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	defer st.Close()

	beadID := "proj-park-sched"
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID:        beadID,
		PID:           1,
		State:         registry.StateParked,
		StartedAt:     time.Now().Add(-5 * time.Minute),
		LastHeartbeat: time.Now().Add(-30 * time.Second),
		ParkSince:     time.Now().Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	o := NewOverseer(DefaultConfig())
	o.registryStore = st
	o.workerSpawner.SetRegistryStore(st)

	s := NewScheduler(o)
	node := &Node{Bead: types.Bead{ID: types.BeadID(beadID), Project: "proj", Type: "bug"}}
	if s.ShouldSpawnWorker(node) {
		t.Fatal("ShouldSpawnWorker must return false for a parked registry row")
	}
}
