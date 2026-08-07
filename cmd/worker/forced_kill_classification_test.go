package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// Watchdog-forced kills (activity timeout, max total runtime) are provider-side
// infrastructure events: the measured 2026-07-28 hang was a request that never
// came back. But their message lives in cmdErr, not in pi's stderr, so
// classifyFailure never saw it — the worker exited -1, the daemon recorded
// "worker exited with code -1", and the bead was closed-failed as a code
// defect, spawning fix beads that died the same way (watchdog-found:
// hydra-code-x77, 2026-08-07). appendForcedKillReason surfaces the kill reason
// into result.Error so the existing timeout markers classify it as exit 3.

func TestAppendForcedKillReason_ActivityTimeout(t *testing.T) {
	result := &ExecutionResult{Success: false, Error: "empty assistant message"}
	appendForcedKillReason(result, fmt.Errorf("activity timeout: no output for %s", "4m0s"))
	if !strings.Contains(result.Error, "activity timeout: no output for 4m0s") {
		t.Errorf("kill reason not surfaced; Error = %q", result.Error)
	}
	if !strings.Contains(result.Error, "empty assistant message") {
		t.Errorf("original stderr lost; Error = %q", result.Error)
	}
	if got := classifyFailure(result); got != 3 {
		t.Errorf("classifyFailure(activity timeout) = %d, want 3 (infra timeout)", got)
	}
}

func TestAppendForcedKillReason_MaxRuntime(t *testing.T) {
	result := &ExecutionResult{Success: false}
	appendForcedKillReason(result, fmt.Errorf("max total runtime exceeded: %s", "15m0s"))
	if got := classifyFailure(result); got != 3 {
		t.Errorf("classifyFailure(max runtime) = %d, want 3 (infra timeout)", got)
	}
}

func TestAppendForcedKillReason_NilAndExitErrorUntouched(t *testing.T) {
	r := &ExecutionResult{Success: false, Error: "boom"}
	appendForcedKillReason(r, nil)
	if r.Error != "boom" {
		t.Errorf("nil cmdErr mutated Error: %q", r.Error)
	}
	// A real process exit carries its reason in stderr already; appending the
	// generic ExitError string ("exit status 1") would only add noise.
	appendForcedKillReason(r, &exec.ExitError{ProcessState: nil})
	if r.Error != "boom" {
		t.Errorf("ExitError mutated Error: %q", r.Error)
	}
}

func TestAppendForcedKillReason_OrdinaryErrorStillAppended(t *testing.T) {
	r := &ExecutionResult{Success: false}
	appendForcedKillReason(r, errors.New("context canceled"))
	if !strings.Contains(r.Error, "context canceled") {
		t.Errorf("non-watchdog error not surfaced: %q", r.Error)
	}
}
