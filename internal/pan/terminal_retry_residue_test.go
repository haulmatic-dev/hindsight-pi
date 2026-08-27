package pan

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// TestCandidatesTerminalRetryResidueSkipsTerminal verifies that a bead that
// reached a terminal state but still has stale scheduler-level retry or spawn
// residue never reaches the judgment prompt.
func TestCandidatesTerminalRetryResidueSkipsTerminal(t *testing.T) {
	store := &fakeStore{} // no live worker row
	s := New(Config{Enabled: true}, store, nil)
	s.SetTerminalBead(func(beadID string) bool {
		return beadID == "terminal-retry"
	})
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{
			{BeadID: "terminal-retry", Reason: "retry exhausted after 5 attempts: example", Since: time.Now().Add(-5 * time.Minute)},
			{BeadID: "still-retry", Reason: "retry exhausted after 5 attempts: example", Since: time.Now().Add(-5 * time.Minute)},
		}
	})

	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	got := make(map[string]bool)
	for _, c := range cands {
		got[c.Record.BeadID] = true
	}
	if got["terminal-retry"] {
		t.Errorf("terminal retry-residue bead became a candidate")
	}
	if !got["still-retry"] {
		t.Errorf("non-terminal retry-residue bead missing from candidates")
	}
}

// TestCandidatesTerminalSpawnResidueSkipsTerminal verifies the same for spawn
// failure residue.
func TestCandidatesTerminalSpawnResidueSkipsTerminal(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{hungWorker("terminal-spawn")}}
	s := New(Config{Enabled: true}, store, nil)
	s.SetTerminalBead(func(beadID string) bool {
		return beadID == "terminal-spawn"
	})

	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	for _, c := range cands {
		if c.Record.BeadID == "terminal-spawn" {
			t.Errorf("terminal spawn-residue live worker became a candidate")
		}
	}
}
