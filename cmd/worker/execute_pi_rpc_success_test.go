package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestExecuteWithPi_RPCSuccessReturnsNilError is the regression test for the
// 2026-09-06 hydra-xnx churn loop: in rpc mode a cleanly-ended turn (valid
// verdict, agent_end) ALWAYS dies by signal, because finish() must kill pi —
// pi cannot exit on its own after agent_end. executeWithPi used to return that
// by-design SIGTERM as a non-nil error, so the caller's retry loop treated a
// perfect run as a failure, retried, and the daemon re-prompted the bead
// indefinitely ("PREVIOUS ATTEMPT FAILED: ... signal: terminated" despite
// done:true verdicts).
func TestExecuteWithPi_RPCSuccessReturnsNilError(t *testing.T) {
	tmp := t.TempDir()

	// Fake pi: consume the rpc prompt line, emit a turn_end carrying a valid
	// verdict, then agent_end, then stay alive like real pi — the worker's
	// finish() tears it down by signal.
	fake := filepath.Join(tmp, "fake-pi.sh")
	script := `#!/bin/sh
head -n1 >/dev/null 2>&1 || true
printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"done.\n\nHYDRA_VERDICT: {\"done\": true}"}]}}'
printf '%s\n' '{"type":"agent_end"}'
while :; do sleep 1; done
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake pi: %v", err)
	}

	t.Setenv("HYDRA_PI_PATH", fake)
	t.Setenv("HYDRA_HOME", tmp)

	bead := types.Bead{
		ID:          types.BeadID("test-rpc-success"),
		Project:     "test",
		Title:       "rpc success error suppression",
		Description: "verify a clean rpc turn returns nil error",
		Type:        "fix",
		Priority:    3,
		Status:      types.StatusInProgress,
	}

	piConfig := PiConfig{Enabled: true, Mode: "rpc"}

	done := make(chan struct{})
	var result *ExecutionResult
	var err error
	go func() {
		defer close(done)
		result, err = executeWithPi(bead, piConfig, nil, tmp, nil, promptExtras{})
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("executeWithPi did not return within 30s")
	}

	if err != nil {
		t.Fatalf("executeWithPi returned error for clean rpc turn: %v (the by-design finish() SIGTERM must be suppressed)", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if !result.Success {
		t.Fatalf("result.Success = false, want true; exit=%d error=%q", result.ExitCode, result.Error)
	}
	if result.ExitCode != 0 {
		t.Fatalf("result.ExitCode = %d, want 0", result.ExitCode)
	}
	v := parseVerdict(result.Output)
	if v == nil || !v.Done {
		t.Fatalf("verdict not captured from turn output: %q", result.Output)
	}
}
