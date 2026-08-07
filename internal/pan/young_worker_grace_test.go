package pan

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// A worker younger than 2×HungAfter must never be a hung candidate: its
// first turn legitimately shows TokensSoFar=0 (tokens land at turn end) and,
// during the registration race, a heartbeat older than the worker itself.
// Pan abandoned exactly such a worker live on 2026-08-07
// (srv-smart-engine-pes, 2m20s into a healthy turn, exit 1, bead fault).
func TestCandidatesYoungWorkerGrace(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{
		{BeadID: "young-stale-looking", State: registry.StateWorking,
			StartedAt: time.Now().Add(-2 * time.Minute),
			// Heartbeat gap exceeds HungAfter (registration race) but the
			// worker is only 2m old — must be given grace.
			LastHeartbeat: time.Now().Add(-6 * time.Minute)},
		{BeadID: "old-hung", State: registry.StateWorking,
			StartedAt:     time.Now().Add(-30 * time.Minute),
			LastHeartbeat: time.Now().Add(-30 * time.Minute)},
	}}

	s := New(Config{Enabled: true}, store, nil)
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	got := map[string]bool{}
	for _, c := range cands {
		got[c.Record.BeadID] = true
	}
	if got["young-stale-looking"] {
		t.Errorf("young worker (2m old) must not be a hung candidate, got %v", got)
	}
	if !got["old-hung"] {
		t.Errorf("old hung worker must remain a candidate, got %v", got)
	}
}
