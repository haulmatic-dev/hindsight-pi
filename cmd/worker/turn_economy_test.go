package main

import (
	"os"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestBuildPiPrompt_IncludesWorkspaceGuard(t *testing.T) {
	bead := types.Bead{
		ID:    "test-bead",
		Title: "GREEN: Implement feature",
		Type:  "implement",
	}
	prompt := buildPiPrompt(bead, nil, promptExtras{})

	for _, want := range []string{
		"WORKSPACE:",
		"cwd is your git worktree",
		"never cd to the main checkout",
		"All edits belong in cwd",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing workspace guidance %q", want)
		}
	}
}

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
		"same message",    // batch independent tool calls together
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
	if !strings.Contains(prompt, "VERIFY LOOP BUDGET") {
		t.Error("HYDRA_DAEMON_GATES=1 must add the verify-loop budget rule")
	}
}

// Verify-loop budget must not appear when the daemon does not own gates.
func TestBuildPiPrompt_VerifyLoopBudgetAbsentWithoutDaemonGates(t *testing.T) {
	os.Unsetenv("HYDRA_DAEMON_GATES")
	prompt := buildPiPrompt(types.Bead{ID: "p-1", Title: "t"}, nil, promptExtras{})
	if strings.Contains(prompt, "VERIFY LOOP BUDGET") {
		t.Error("VERIFY LOOP BUDGET must not appear when HYDRA_DAEMON_GATES is unset")
	}
}
