package core

import (
	"errors"
	"testing"
	"time"
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

func TestScheduler_ShouldPauseForCongestion_BelowThreshold(t *testing.T) {
	s := &Scheduler{
		congestionCounter: func() (int, error) { return 2, nil },
	}
	paused, err := s.shouldPauseForCongestion()
	if err != nil {
		t.Fatalf("shouldPauseForCongestion: %v", err)
	}
	if paused {
		t.Fatal("expected no pause below threshold")
	}
}
