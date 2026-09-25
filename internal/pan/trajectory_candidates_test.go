package pan

import (
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// workingWorker is a healthy-looking working worker started long enough ago
// to pass the trajectory grace window.
func workingWorker(beadID string) registry.WorkerRecord {
	return registry.WorkerRecord{
		BeadID:        beadID,
		State:         registry.StateWorking,
		StartedAt:     time.Now().Add(-30 * time.Minute),
		LastHeartbeat: time.Now(),
	}
}

func TestCandidatesTrajectoryOutlierSurfaces(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{workingWorker("looping")}}
	s := New(Config{
		Enabled: true,
		TrajectoryFunc: func(beadID string) (Trajectory, bool) {
			return Trajectory{Turns: 90, RepeatedCommand: "go vet ./...", RepeatedCount: 12}, true
		},
		BeadClass: func(beadID string) string { return "syntax" },
	}, store, nil)
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("want 1 trajectory candidate, got %d", len(cands))
	}
	if !strings.HasPrefix(cands[0].Reason, "TRAJECTORY: ") {
		t.Fatalf("reason = %q, want TRAJECTORY prefix", cands[0].Reason)
	}
}

func TestCandidatesTrajectoryHealthyNotCandidate(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{workingWorker("steady")}}
	s := New(Config{
		Enabled: true,
		TrajectoryFunc: func(beadID string) (Trajectory, bool) {
			return Trajectory{Turns: 12, TestRuns: 2}, true
		},
	}, store, nil)
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("healthy trajectory must never be a candidate, got %v", cands)
	}
}

func TestCandidatesTrajectoryYoungWorkerSkipped(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{{
		BeadID: "young", State: registry.StateWorking,
		StartedAt:     time.Now().Add(-2 * time.Minute),
		LastHeartbeat: time.Now(),
	}}}
	s := New(Config{
		Enabled: true,
		TrajectoryFunc: func(beadID string) (Trajectory, bool) {
			return Trajectory{Turns: 999}, true
		},
	}, store, nil)
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("young worker must be skipped by trajectory scan, got %v", cands)
	}
}

func TestCandidatesTrajectoryClassUsed(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{workingWorker("cls")}}
	s := New(Config{
		Enabled: true,
		TrajectoryFunc: func(beadID string) (Trajectory, bool) {
			return Trajectory{Turns: 11}, true
		},
		BeadClass: func(beadID string) string { return "syntax" }, // baseline 5, 2×5=10 < 11
	}, store, nil)
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("syntax-class 11 turns should flag, got %d candidates", len(cands))
	}
	// Same trajectory against the generous default baseline must not flag.
	s2 := New(Config{
		Enabled: true,
		TrajectoryFunc: func(beadID string) (Trajectory, bool) {
			return Trajectory{Turns: 11}, true
		},
	}, store, nil)
	cands2, err := s2.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands2) != 0 {
		t.Fatalf("default-class 11 turns must not flag, got %v", cands2)
	}
}
