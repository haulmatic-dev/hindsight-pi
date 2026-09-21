package main

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestParsePiConfig_TurnBudgetDefaults verifies the worker applies safe
// defaults when the daemon does not pass budget env vars.
func TestParsePiConfig_TurnBudgetDefaults(t *testing.T) {
	t.Setenv("HYDRA_PI_TURN_BUDGET", "")
	t.Setenv("HYDRA_PI_TURN_BUDGET_CADENCE", "")

	cfg := parsePiConfig()
	if got := cfg.turnBudget(); got != 200 {
		t.Errorf("default turn budget = %d, want 200", got)
	}
	if got := cfg.reminderCadence(); got != 1 {
		t.Errorf("default reminder cadence = %d, want 1", got)
	}
}

// TestParsePiConfig_TurnBudgetFromEnv verifies the worker parses the budget
// and cadence forwarded by the daemon.
func TestParsePiConfig_TurnBudgetFromEnv(t *testing.T) {
	t.Setenv("HYDRA_PI_TURN_BUDGET", "150")
	t.Setenv("HYDRA_PI_TURN_BUDGET_CADENCE", "5")

	cfg := parsePiConfig()
	if cfg.TurnBudget != 150 {
		t.Errorf("turn budget = %d, want 150", cfg.TurnBudget)
	}
	if cfg.TurnBudgetCadence != 5 {
		t.Errorf("reminder cadence = %d, want 5", cfg.TurnBudgetCadence)
	}
}

// TestBuildNudgePrompt_ReminderCadence pins the non-binding reminder cadence.
// The reminder is appended only on cadence boundaries and carries the current
// turn count plus the configured soft budget.
func TestBuildNudgePrompt_ReminderCadence(t *testing.T) {
	cfg := PiConfig{TurnBudget: 150, TurnBudgetCadence: 2}

	p1 := buildNudgePrompt("x", 1, cfg)
	if strings.Contains(p1, "Reminder:") {
		t.Error("turn 1 should not carry a budget reminder")
	}

	p2 := buildNudgePrompt("x", 2, cfg)
	if !strings.Contains(p2, "Reminder:") {
		t.Error("turn 2 on cadence 2 must carry a budget reminder")
	}
	if !strings.Contains(p2, "used 2 of ~150 turns") {
		t.Errorf("reminder text missing expected content: %q", p2)
	}

	p3 := buildNudgePrompt("x", 3, cfg)
	if strings.Contains(p3, "Reminder:") {
		t.Error("turn 3 off cadence should not carry a reminder")
	}
}

// TestBuildPiPrompt_TurnBudgetBlock verifies the static prompt renders the
// per-phase soft budget and advertises the dynamic reminder cadence.
func TestBuildPiPrompt_TurnBudgetBlock(t *testing.T) {
	bead := types.Bead{ID: "b1", Type: "task", Title: "GREEN: x"}
	cfg := PiConfig{TurnBudget: 300, TurnBudgetCadence: 1}
	p := buildPiPrompt(bead, nil, promptExtras{}, cfg)

	if !strings.Contains(p, "TURN BUDGET") {
		t.Error("prompt missing TURN BUDGET block")
	}
	if !strings.Contains(p, "~300 turns") {
		t.Errorf("prompt budget not rendered, got: %q", p)
	}
	if !strings.Contains(p, "You have used N of ~300 turns") {
		t.Errorf("prompt missing dynamic reminder note")
	}
}
