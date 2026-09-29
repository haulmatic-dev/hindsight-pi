package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// FR-021: quarantine-labeled (stranded-conflict) open beads must surface as
// Pan blocked candidates; escalations younger than 24h must not.
func TestPanBlockedReason(t *testing.T) {
	now := time.Now()

	// Open bead quarantined by integrate-stranded: must yield a candidate.
	b := types.Bead{
		ID: types.BeadID("srv-1"), Status: types.StatusOpen,
		Labels:    []string{strandedConflictLabel},
		UpdatedAt: now.Add(-2 * time.Hour),
	}
	reason, ok := panBlockedReason(b, now)
	if !ok {
		t.Fatal("stranded-conflict bead must surface as a Pan candidate")
	}
	if reason == "" {
		t.Fatal("reason must not be empty")
	}

	// Terminal bead never surfaces (guard lives in the sweep, but the helper
	// must not contradict it).
	b.Status = types.StatusClosed
	if _, ok := panBlockedReason(b, now); ok {
		t.Fatal("terminal quarantine-labeled bead must not surface")
	}

	// Escalated label younger than 24h: no candidate.
	b = types.Bead{
		ID: types.BeadID("srv-2"), Status: types.StatusOpen,
		Labels:    []string{escalatedLabel},
		UpdatedAt: now.Add(-2 * time.Hour),
	}
	if _, ok := panBlockedReason(b, now); ok {
		t.Fatal("recent escalation must not surface before 24h")
	}

	// Escalated label older than 24h: candidate.
	b.UpdatedAt = now.Add(-25 * time.Hour)
	if reason, ok := panBlockedReason(b, now); !ok || reason == "" {
		t.Fatal("stale escalation (>24h) must surface")
	}

	// Hard-park labels still surface.
	b = types.Bead{
		ID: types.BeadID("srv-3"), Status: types.StatusOpen,
		Labels:    []string{worktreeUnavailableLabel},
		UpdatedAt: now,
	}
	if _, ok := panBlockedReason(b, now); !ok {
		t.Fatal("hard-parked bead must surface")
	}
}
