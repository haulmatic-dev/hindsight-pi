package pan

import (
	"testing"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

func TestCandidatesSkipsTerminalBeads(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{
		hungWorker("live"),
		hungWorker("terminal"),
		hungWorker("error"),
	}}

	s := New(Config{Enabled: true}, store, nil)
	s.SetTerminalBead(func(beadID string) bool {
		switch beadID {
		case "terminal":
			return true
		case "error":
			return false // error path must fail open and include the candidate
		default:
			return false
		}
	})

	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	got := map[string]bool{}
	for _, c := range cands {
		got[c.Record.BeadID] = true
	}
	if !got["live"] {
		t.Errorf("live bead should be a candidate")
	}
	if !got["error"] {
		t.Errorf("error bead should be included when TerminalBead returns false")
	}
	if got["terminal"] {
		t.Errorf("terminal bead should be skipped")
	}
}

func TestCandidatesIncludesWhenTerminalBeadNil(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{
		hungWorker("terminal"),
	}}

	s := New(Config{Enabled: true}, store, nil)
	// TerminalBead left nil: every candidate must be included (fail open).
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 1 || cands[0].Record.BeadID != "terminal" {
		t.Errorf("expected terminal bead to be included when dep is nil, got %+v", cands)
	}
}

func TestCandidatesTerminalBeadSkippedSilently(t *testing.T) {
	// Ensure TerminalBead returning true suppresses the candidate without
	// affecting others.
	store := &fakeStore{workers: []registry.WorkerRecord{
		hungWorker("a"),
		hungWorker("b"),
	}}
	called := 0
	s := New(Config{Enabled: true}, store, nil)
	s.SetTerminalBead(func(beadID string) bool {
		called++
		return beadID == "a"
	})
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if called != 2 {
		t.Errorf("TerminalBead called %d times, want 2", called)
	}
	if len(cands) != 1 || cands[0].Record.BeadID != "b" {
		t.Errorf("expected only bead b, got %+v", cands)
	}
}

func TestCandidatesBlockedBeadsSkipsTerminalBead(t *testing.T) {
	store := &fakeStore{}
	s := New(Config{Enabled: true}, store, nil)
	s.SetTerminalBead(func(beadID string) bool {
		return beadID == "terminal-blocked"
	})
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{
			{BeadID: "terminal-blocked", Reason: "retry exhausted"},
			{BeadID: "still-blocked", Reason: "spawn failed"},
		}
	})

	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	got := map[string]bool{}
	for _, c := range cands {
		got[c.Record.BeadID] = true
	}
	if got["terminal-blocked"] {
		t.Errorf("terminal blocked bead became a candidate")
	}
	if !got["still-blocked"] {
		t.Errorf("non-terminal blocked bead missing")
	}
}

// Test for the core service wiring helper would live in internal/core,
// but the Candidates logic itself is covered above.
