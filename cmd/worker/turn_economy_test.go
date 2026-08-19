package main

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Turn economy: pi's RPC turn path records exactly one assistant message per
// tool call, and every turn re-bills the entire transcript. Session forensics
// on srv-smart-engine (2026-08-17, bead co3u8) showed 167 turns / 166 unique
// tool calls / 25M tokens — 24.8M of it cache reads of the ever-growing
// transcript (16K at turn 0, 244K by turn 167). Turn count is the linear
// multiplier of the quadratic tax: fleet p50 is 89 turns/bead, p95 is 408.
// The single biggest behavioral lever is batching: independent tool calls in
// ONE assistant message, one comprehensive probe instead of a probe sequence,
// and no full-suite re-runs between micro-edits.
func TestBuildPiPrompt_IncludesTurnEconomy(t *testing.T) {
	bead := types.Bead{
		ID:    "test-bead",
		Title: "GREEN: Implement feature",
		Type:  "implement",
	}
	prompt := buildPiPrompt(bead, nil, promptExtras{})

	for _, want := range []string{
		"TURN ECONOMY",
		"same message",   // batch independent tool calls together
		"full test suite", // don't re-run per micro-edit
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing turn-economy guidance %q", want)
		}
	}
}

// Daemon-owned gates: the prompt must redirect the agent away from full-suite
// babysitting when (and only when) the daemon owns verification.
func TestBuildPiPrompt_DaemonGatesNote(t *testing.T) {
	t.Setenv("HYDRA_DAEMON_GATES", "1")
	prompt := buildPiPrompt(types.Bead{ID: "p-1", Title: "t"}, nil, promptExtras{})
	if !strings.Contains(prompt, "VERIFICATION OWNERSHIP") {
		t.Error("HYDRA_DAEMON_GATES=1 must add the verification-ownership note")
	}
}
