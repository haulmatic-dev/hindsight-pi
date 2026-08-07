package main

import (
	"testing"
	"time"

	piagents "github.com/haulmatic-dev/hydra/internal/agents"
	"github.com/haulmatic-dev/hydra/internal/registry"
)

// Pi emits per-message usage on its rpc stream (an execution produced 8,009
// usage events totalling tens of thousands of tokens) yet the registry always
// showed tokens_so_far = 0 — nothing ever forwarded the parsed session stats.
// Pan's stuck heuristics read tokens, so a silent reporter produces false
// "zero tokens" stuck verdicts.
func TestReportTurnTokens_ForwardsSessionTotal(t *testing.T) {
	old := activeReporter
	rep := newStateReporter(registry.NewClient("http://127.0.0.1:1"), "bead-x", "proj")
	activeReporter = rep
	defer func() { activeReporter = old }()

	reportTurnTokens(&piagents.SessionStats{Input: 100, Output: 4, Total: 42104})
	if got := rep.rep.TokensSoFar; got != 42104 {
		t.Errorf("TokensSoFar = %d, want 42104 (stats never forwarded)", got)
	}

	// nil stats and nil reporter must be safe no-ops (park/abandon paths).
	reportTurnTokens(nil)
	if got := rep.rep.TokensSoFar; got != 42104 {
		t.Errorf("TokensSoFar changed on nil stats: %d", got)
	}
	activeReporter = nil
	reportTurnTokens(&piagents.SessionStats{Total: 1})
}

// Stop must flush a final full heartbeat BEFORE the bare completion marker:
// workers exit <30s after the last turn, so without the flush the parsed
// token total (SetTokens) never reaches the registry — observed live on
// hydra-code-zuo: worker logged total=616789, registry showed 0.
func TestReporterStop_FlushesFinalState(t *testing.T) {
	rep := newStateReporter(registry.NewClient("http://127.0.0.1:1"), "bead-flush", "proj")
	rep.rep.TokensSoFar = 616789
	rep.Start() // Stop waits on the Start goroutine's stopped channel

	done := make(chan struct{})
	go func() { rep.Stop(0); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Stop hung (fail-open violated)")
	}

	if rep.rep.State != string(registry.StateCompleted) {
		t.Errorf("State = %q, want %q after Stop(0)", rep.rep.State, registry.StateCompleted)
	}
	if rep.rep.TokensSoFar != 616789 {
		t.Errorf("TokensSoFar = %d, want 616789 preserved for the final flush", rep.rep.TokensSoFar)
	}
}
