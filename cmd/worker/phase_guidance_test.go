package main

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestPhaseGuidance pins the per-phase doctrine routing: each TDD phase gets
// its own discipline, fix beads get the root-cause protocol, and phases
// without special discipline get nothing.
func TestPhaseGuidance(t *testing.T) {
	cases := []struct {
		title, beadType, want string
	}{
		{"RED: Write tests for X", "task", "fails for the RIGHT reason"},
		{"GREEN: Implement X", "task", "MINIMUM that makes the failing tests pass"},
		{"REFACTOR: Optimize X", "task", "Behavior must not change"},
		{"Fix: COMPLIANCE GATE", "fix", "root-cause protocol"},
		{"anything", "fix", "root-cause protocol"},
		{"Discovery: X", "task", ""},
		{"SYNTAX: Check X", "task", ""},
		{"EPIC: X", "epic", ""},
	}
	for _, tc := range cases {
		g := phaseGuidance(types.Bead{Title: tc.title, Type: tc.beadType})
		if tc.want == "" {
			if g != "" {
				t.Errorf("%q/%s: expected no guidance, got %q", tc.title, tc.beadType, g[:40])
			}
			continue
		}
		if !strings.Contains(g, tc.want) {
			t.Errorf("%q/%s: guidance missing %q", tc.title, tc.beadType, tc.want)
		}
	}
}

// TestBuildPiPrompt_IncludesPhaseGuidance verifies the guidance lands in the
// final prompt.
func TestBuildPiPrompt_IncludesPhaseGuidance(t *testing.T) {
	p := buildPiPrompt(types.Bead{Type: "fix", Title: "Fix: broken gate", Description: "d"}, nil, promptExtras{})
	if !strings.Contains(p, "REPRODUCE") || !strings.Contains(p, "root-cause protocol") {
		t.Error("fix-bead prompt must carry the root-cause protocol")
	}
	p2 := buildPiPrompt(types.Bead{Type: "task", Title: "Discovery: X", Description: "d"}, nil, promptExtras{})
	if strings.Contains(p2, "root-cause protocol") {
		t.Error("non-fix beads must not carry fix doctrine")
	}
}
