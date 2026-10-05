package core

import (
	"strings"
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
	reason, ok := panBlockedReason(b, now, time.Hour, 0)
	if !ok {
		t.Fatal("stranded-conflict bead must surface as a Pan candidate")
	}
	if reason == "" {
		t.Fatal("reason must not be empty")
	}

	// Terminal bead never surfaces (guard lives in the sweep, but the helper
	// must not contradict it).
	b.Status = types.StatusClosed
	if _, ok := panBlockedReason(b, now, time.Hour, 0); ok {
		t.Fatal("terminal quarantine-labeled bead must not surface")
	}

	// Escalated label younger than 24h: no candidate.
	b = types.Bead{
		ID: types.BeadID("srv-2"), Status: types.StatusOpen,
		Labels:    []string{escalatedLabel},
		UpdatedAt: now.Add(-2 * time.Hour),
	}
	if _, ok := panBlockedReason(b, now, time.Hour, 0); ok {
		t.Fatal("recent escalation must not surface before 24h")
	}

	// Escalated label older than 24h: candidate.
	b.UpdatedAt = now.Add(-25 * time.Hour)
	if reason, ok := panBlockedReason(b, now, time.Hour, 0); !ok || reason == "" {
		t.Fatal("stale escalation (>24h) must surface")
	}

	// Hard-park labels still surface.
	b = types.Bead{
		ID: types.BeadID("srv-3"), Status: types.StatusOpen,
		Labels:    []string{worktreeUnavailableLabel},
		UpdatedAt: now,
	}
	if _, ok := panBlockedReason(b, now, time.Hour, 0); !ok {
		t.Fatal("hard-parked bead must surface")
	}
}

// Wedge fast-path (2026-10-05 srv-jobfile stall): an escalated bead with live
// dependents surfaces after wedgeGrace, not 24h — every escalation is an
// orphaned in_progress by construction, so a chain head starves the subtree
// while a leaf can wait for a human.
func TestPanBlockedReasonEscalatedWedge(t *testing.T) {
	now := time.Now()
	mk := func(age time.Duration) types.Bead {
		return types.Bead{
			ID: types.BeadID("srv-w"), Status: types.StatusInProgress,
			Labels:    []string{escalatedLabel, "needs-human"},
			UpdatedAt: now.Add(-age),
		}
	}

	// Wedged + past wedge grace: candidate, reason names the wedge.
	if reason, ok := panBlockedReason(mk(2*time.Hour), now, time.Hour, 16); !ok {
		t.Fatal("wedged escalation past grace must surface")
	} else if !strings.Contains(reason, "WEDGING 16") {
		t.Fatalf("reason must report the dependent count, got: %s", reason)
	}

	// Wedged but within grace: no candidate (give the human the grace window).
	if _, ok := panBlockedReason(mk(30*time.Minute), now, time.Hour, 16); ok {
		t.Fatal("wedged escalation within grace must not surface yet")
	}

	// Leaf escalation (no dependents): wedge path does not apply, 24h rule stands.
	if _, ok := panBlockedReason(mk(2*time.Hour), now, time.Hour, 0); ok {
		t.Fatal("leaf escalation younger than 24h must not surface")
	}
	if _, ok := panBlockedReason(mk(25*time.Hour), now, time.Hour, 0); !ok {
		t.Fatal("leaf escalation older than 24h must surface")
	}

	// Zero/negative grace disables the wedge path (fail back to 24h rule).
	if _, ok := panBlockedReason(mk(2*time.Hour), now, 0, 16); ok {
		t.Fatal("wedgeGrace<=0 must disable the wedge path")
	}
}
