package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/internal/validation"
)

// Validation churn: every failed round spawned a fix bead per gap at the
// PARENT bead's priority — including advisory Warning gaps like "Missing
// Acceptance Criteria" (fires whenever the root bead lacks AC text) and
// metadata-absence Criticals like "No Implementation Files" (leaf beads lack
// target_files metadata even when the work exists and is green). On
// srv-smart-engine these meta-beads outcompeted real PRD work: ~25% of
// commits in a day were validation-round fixes and the open board grew
// instead of draining.
//
// Decision rule: Warning gaps are advisory — log, never spawn. Spawned fix
// beads are floored at P3 so they never queue-jump fresh PRD work.
func TestValidationFixDecision(t *testing.T) {
	cases := []struct {
		name     string
		severity validation.ErrorSeverity
		parentP  int
		spawn    bool
		priority int
	}{
		{"warning is advisory, no bead", validation.SeverityWarning, 0, false, 0},
		{"error spawns, P0 parent floored to P3", validation.SeverityError, 0, true, 3},
		{"critical spawns, P1 parent floored to P3", validation.SeverityCritical, 1, true, 3},
		{"P2 parent floored to P3", validation.SeverityError, 2, true, 3},
		{"P3 parent stays P3", validation.SeverityError, 3, true, 3},
		{"P4 parent stays P4 (floor never lifts)", validation.SeverityError, 4, true, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spawn, prio := validationFixDecision(tc.severity, tc.parentP)
			if spawn != tc.spawn {
				t.Errorf("spawn = %v, want %v", spawn, tc.spawn)
			}
			if spawn && prio != tc.priority {
				t.Errorf("priority = %d, want %d", prio, tc.priority)
			}
		})
	}
}
