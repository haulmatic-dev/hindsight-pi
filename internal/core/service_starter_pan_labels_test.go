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
	reason, ok := panBlockedReason(b, now, time.Hour, 24*time.Hour, 0)
	if !ok {
		t.Fatal("stranded-conflict bead must surface as a Pan candidate")
	}
	if reason == "" {
		t.Fatal("reason must not be empty")
	}

	// Terminal bead never surfaces (guard lives in the sweep, but the helper
	// must not contradict it).
	b.Status = types.StatusClosed
	if _, ok := panBlockedReason(b, now, time.Hour, 24*time.Hour, 0); ok {
		t.Fatal("terminal quarantine-labeled bead must not surface")
	}

	// Escalated label younger than 24h: no candidate.
	b = types.Bead{
		ID: types.BeadID("srv-2"), Status: types.StatusOpen,
		Labels:    []string{escalatedLabel},
		UpdatedAt: now.Add(-2 * time.Hour),
	}
	if _, ok := panBlockedReason(b, now, time.Hour, 24*time.Hour, 0); ok {
		t.Fatal("recent escalation must not surface before 24h")
	}

	// Escalated label older than 24h: candidate.
	b.UpdatedAt = now.Add(-25 * time.Hour)
	if reason, ok := panBlockedReason(b, now, time.Hour, 24*time.Hour, 0); !ok || reason == "" {
		t.Fatal("stale escalation (>24h) must surface")
	}

	// Hard-park labels still surface.
	b = types.Bead{
		ID: types.BeadID("srv-3"), Status: types.StatusOpen,
		Labels:    []string{worktreeUnavailableLabel},
		UpdatedAt: now,
	}
	if _, ok := panBlockedReason(b, now, time.Hour, 24*time.Hour, 0); !ok {
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
	if reason, ok := panBlockedReason(mk(2*time.Hour), now, time.Hour, 24*time.Hour, 16); !ok {
		t.Fatal("wedged escalation past grace must surface")
	} else if !strings.Contains(reason, "WEDGING 16") {
		t.Fatalf("reason must report the dependent count, got: %s", reason)
	}

	// Wedged but within grace: no candidate (give the human the grace window).
	if _, ok := panBlockedReason(mk(30*time.Minute), now, time.Hour, 24*time.Hour, 16); ok {
		t.Fatal("wedged escalation within grace must not surface yet")
	}

	// Leaf escalation (no dependents): wedge path does not apply, leaf grace stands.
	if _, ok := panBlockedReason(mk(2*time.Hour), now, time.Hour, 24*time.Hour, 0); ok {
		t.Fatal("leaf escalation younger than 24h must not surface")
	}
	if _, ok := panBlockedReason(mk(25*time.Hour), now, time.Hour, 24*time.Hour, 0); !ok {
		t.Fatal("leaf escalation older than 24h must surface")
	}

	// Zero/negative grace disables the wedge path (fail back to leaf rule).
	if _, ok := panBlockedReason(mk(2*time.Hour), now, 0, 24*time.Hour, 16); ok {
		t.Fatal("wedgeGrace<=0 must disable the wedge path")
	}
}

// Leaf grace knob (srv-jobfile-eci, 2026-10-05): an escalated leaf whose
// cause class was a daemon defect should be auto-retried shortly after the
// fix deploys, not idled overnight waiting on the historical 24h human
// window. pan.escalated_leaf_grace_seconds controls the threshold; the
// reason carries the repair:clear-escalation preference so Pan acts.
func TestPanBlockedReasonLeafGrace(t *testing.T) {
	now := time.Now()
	mk := func(age time.Duration) types.Bead {
		return types.Bead{
			ID: types.BeadID("srv-eci"), Status: types.StatusInProgress,
			Labels:    []string{escalatedLabel, "needs-human"},
			UpdatedAt: now.Add(-age),
		}
	}

	// Past a 1h leaf grace: candidate, reason prefers clear-escalation.
	reason, ok := panBlockedReason(mk(9*time.Hour), now, time.Hour, time.Hour, 0)
	if !ok {
		t.Fatal("leaf past leafGrace must surface")
	}
	if !strings.Contains(reason, "repair:clear-escalation") {
		t.Fatalf("leaf reason must prefer repair:clear-escalation, got: %s", reason)
	}

	// Within leaf grace: no candidate.
	if _, ok := panBlockedReason(mk(30*time.Minute), now, time.Hour, time.Hour, 0); ok {
		t.Fatal("leaf within leafGrace must not surface")
	}

	// Zero/negative leafGrace falls back to the 24h default.
	if _, ok := panBlockedReason(mk(2*time.Hour), now, time.Hour, 0, 0); ok {
		t.Fatal("leafGrace<=0 must fall back to 24h, not surface a 2h leaf")
	}
	if _, ok := panBlockedReason(mk(25*time.Hour), now, time.Hour, 0, 0); !ok {
		t.Fatal("leafGrace<=0 must fall back to 24h, surfacing a 25h leaf")
	}
}
