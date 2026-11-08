package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Close-time coherence (hydra-lqce M3): a completed close contradicts a
// pre-existing closed-failed verdict (mqn, 2026-10-16: closed completed
// while carrying closed-failed — the stale label quarantined a 132-bead
// chain). The completed close must scrub it.
func TestBeadCloser_CompletedCloseScrubsStaleClosedFailed(t *testing.T) {
	mock := newMockBeadsClient()
	mock.addBead(types.Bead{
		ID:     types.BeadID("demo/bead-1"),
		Status: types.StatusOpen,
		Type:   "task",
		Labels: []string{closedFailedLabel},
	})

	resolver := setupTestProjectResolver(t, "demo", t.TempDir())
	closer := NewBeadCloser(mock, NewIntelligenceHub(IntelligenceConfig{}), resolver, nil, &Config{})
	closer.SetEventSink(NewEventSink(t.TempDir()))

	worker := &types.Worker{BeadID: types.BeadID("demo/bead-1")}
	if err := closer.Close(worker, true, 0); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	bead := mock.beads["demo/bead-1"]
	if hasBeadLabel(bead.Labels, closedFailedLabel) {
		t.Errorf("closed-failed survived a completed close: %v", bead.Labels)
	}
	if !hasBeadLabel(bead.Labels, closedFailedRevalidatedLabel) {
		t.Errorf("closed-failed-revalidated audit label missing: %v", bead.Labels)
	}
}

func TestBeadCloser_FailedCloseKeepsClosedFailed(t *testing.T) {
	mock := newMockBeadsClient()
	mock.addBead(types.Bead{
		ID:     types.BeadID("demo/bead-2"),
		Status: types.StatusOpen,
		Type:   "task",
	})

	resolver := setupTestProjectResolver(t, "demo", t.TempDir())
	closer := NewBeadCloser(mock, NewIntelligenceHub(IntelligenceConfig{}), resolver, nil, &Config{})
	closer.SetEventSink(NewEventSink(t.TempDir()))

	worker := &types.Worker{BeadID: types.BeadID("demo/bead-2")}
	if err := closer.Close(worker, false, 1); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	bead := mock.beads["demo/bead-2"]
	if !hasBeadLabel(bead.Labels, closedFailedLabel) {
		t.Errorf("failed close must record closed-failed: %v", bead.Labels)
	}
	if hasBeadLabel(bead.Labels, closedFailedRevalidatedLabel) {
		t.Errorf("failed close must NOT mark revalidated: %v", bead.Labels)
	}
}
