package main

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// RED: a live rpc session emits turn_end events carrying per-message usage
// long before the run exits, yet the registry row showed tokens_so_far = 0
// for the whole session (PID 2353988 class: an hours-long chain with zero
// ever-tokens — Charon's zero-token stall probe flagged a WORKING worker and
// Pan filed false kill-stalled-worker candidates). The rpc scanner must
// forward parsed usage totals to the reporter on every usage-bearing event,
// not only at exit.

// feedRPCLines drives a fake rpc session: the same lines the scanner loop
// feeds to turn.handleLine, one usage-bearing turn_end plus a later message
// event, so a green scanner goroutine forwards tokens well within one
// heartbeat interval (reporterInterval = 30s).
func feedRPCLines(turn *rpcTurn, lines []string) {
	for _, line := range lines {
		turn.handleLine(line, nil)
	}
}

// waitForTokens polls up to one heartbeat interval for TokensSoFar > 0.
func waitForTokens(rep *stateReporter, within time.Duration) int64 {
	deadline := time.Now().Add(within)
	for {
		rep.mu.Lock()
		got := rep.rep.TokensSoFar
		rep.mu.Unlock()
		if got > 0 {
			return got
		}
		if time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRPCTurnEndUsageForwardsTokensWithinHeartbeat(t *testing.T) {
	old := activeReporter
	rep := newStateReporter(registry.NewClient("http://127.0.0.1:1"), "bead-rpc-live", "proj")
	activeReporter = rep
	defer func() { activeReporter = old }()

	turn := newRPCTurn(false)
	feedRPCLines(turn, []string{
		`{"type":"turn_start"}`,
		// turn_end carrying per-message usage (input>0, output>0) — the real
		// session PID 2353988 emitted hundreds of these while the registry
		// showed zero ever-tokens for the whole run.
		`{"type":"turn_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"working"}],"usage":{"input_tokens":1200,"cache_read_input_tokens":900,"output_tokens":40}}}`,
	})

	if got := waitForTokens(rep, reporterInterval); got <= 0 {
		t.Fatalf("TokensSoFar = %d after a usage-bearing turn_end, want >0 within one heartbeat interval (rpc scanner forwards nothing until exit)", got)
	}
}

func TestRPCTurnUsageForwardsOnEveryUsageBearingEvent(t *testing.T) {
	old := activeReporter
	rep := newStateReporter(registry.NewClient("http://127.0.0.1:1"), "bead-rpc-accum", "proj")
	activeReporter = rep
	defer func() { activeReporter = old }()

	turn := newRPCTurn(false)
	feedRPCLines(turn, []string{
		// First usage-bearing message event.
		`{"type":"message","message":{"role":"assistant","usage":{"input_tokens":1000,"cache_read_input_tokens":900,"output_tokens":10}}}`,
	})
	if got := waitForTokens(rep, reporterInterval); got <= 0 {
		t.Fatalf("TokensSoFar = %d after first usage-bearing message event, want >0 (forwarding must not wait for exit)", got)
	}

	rep.mu.Lock()
	first := rep.rep.TokensSoFar
	rep.mu.Unlock()

	feedRPCLines(turn, []string{
		// A later usage-bearing event must update the reporter again, not
		// stick at the first forwarding (every usage-bearing event forwards).
		`{"type":"message","message":{"role":"assistant","usage":{"input_tokens":500,"output_tokens":20}}}`,
	})
	deadline := time.Now().Add(reporterInterval)
	for {
		rep.mu.Lock()
		got := rep.rep.TokensSoFar
		rep.mu.Unlock()
		if got > first {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("TokensSoFar = %d after second usage-bearing event, want >%d (forwarding must happen on every usage-bearing event)", got, first)
		}
		time.Sleep(5 * time.Millisecond)
	}

	rep.mu.Lock()
	rep2 := rep.rep
	rep.mu.Unlock()
	if rep2.TokensFresh <= 0 {
		t.Errorf("TokensFresh = %d, want >0: the breakdown fields must ride the same forwarding", rep2.TokensFresh)
	}
	if rep2.TokensOutput <= 0 {
		t.Errorf("TokensOutput = %d, want >0: output tokens must ride the same forwarding", rep2.TokensOutput)
	}
}

// A usage-free rpc session must NOT fabricate tokens: forwarding is driven
// by usage-bearing events only.
func TestRPCTurnNoUsageEventLeavesTokensZero(t *testing.T) {
	old := activeReporter
	rep := newStateReporter(registry.NewClient("http://127.0.0.1:1"), "bead-rpc-silent", "proj")
	activeReporter = rep
	defer func() { activeReporter = old }()

	turn := newRPCTurn(false)
	feedRPCLines(turn, []string{
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"no usage block here"}]}}`,
	})
	time.Sleep(50 * time.Millisecond)
	rep.mu.Lock()
	got := rep.rep.TokensSoFar
	rep.mu.Unlock()
	if got != 0 {
		t.Fatalf("TokensSoFar = %d, want 0: a session with no usage-bearing event must stay zero", got)
	}
}
