package main

import (
	"testing"
	"time"
)

// Regression: execution reports in /tmp/hydra-executions showed duration=0
// and attempts=0 because writeExecutionOutputToFile ran inside executeWithPi,
// before the caller set result.Duration and result.Attempts.
func TestExecutionReportRoundTrip(t *testing.T) {
	result := &ExecutionResult{
		Success:     true,
		ExitCode:    0,
		Duration:    7 * time.Minute,
		Attempts:    3,
		TurnCount:   12,
		AgentType:   "pi",
		Output:      "out",
		TokensTotal: 1234,
		TokensFresh: 567,
	}

	path, err := writeExecutionOutputToFile("telem-test", result)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	got, err := readExecutionOutputFromFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if got.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", got.Attempts)
	}
	if got.TurnCount != 12 {
		t.Errorf("turns = %d, want 12", got.TurnCount)
	}
	if got.Duration != 7*time.Minute {
		t.Errorf("duration = %v, want 7m", got.Duration)
	}
	if got.TokensTotal != 1234 {
		t.Errorf("tokens_total = %d, want 1234", got.TokensTotal)
	}
}