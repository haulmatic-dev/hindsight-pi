package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED: the worker never counts the rpc turn_end events a live Pi session
// emits, so the heartbeat report carries tokens_so_far but no turn counter,
// and the close path emits nothing about turns. GREEN must implement the
// contract pinned below (see cmd/worker/turn_telemetry.go).

const turnTelemetryEnforceEnv = "TURNS_TELEMETRY_ENFORCE"

func turnTelemetryRequireImpl(t *testing.T) {
	t.Helper()
	if turnTelemetryImplemented {
		return
	}
	if os.Getenv(turnTelemetryEnforceEnv) == "" {
		t.Skipf("RED: worker turn telemetry contract not yet implemented (set %s=1 to enforce)", turnTelemetryEnforceEnv)
	}
	t.Errorf("RED: worker turn telemetry contract not yet implemented")
}

// feedTurnLines drives a fake rpc session: the same lines the scanner loop
// feeds to turn.handleLine. Session A emits 3 turn_end events (three agent
// turns), session B emits none.
var threeTurnEndLines = []string{
	`{"type":"turn_start"}`,
	`{"type":"turn_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"first"}],"usage":{"input_tokens":100,"output_tokens":5}}}`,
	`{"type":"turn_start"}`,
	`{"type":"turn_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"second"}],"usage":{"input_tokens":100,"output_tokens":5}}}`,
	`{"type":"turn_start"}`,
	`{"type":"turn_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"third"}],"usage":{"input_tokens":100,"output_tokens":5}}}`,
}

func waitForTurns(t *testing.T, rep *stateReporter, want int64, within time.Duration) int64 {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		rep.mu.Lock()
		got := rep.rep.TurnsSoFar
		rep.mu.Unlock()
		if got >= want {
			return got
		}
		if time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// AC1 (RED): a fake rpc session streaming 3 turn_end events must produce
// TurnsSoFar=3 in the heartbeat report, alongside TokensSoFar.
func TestTurnTelemetry_ThreeTurnEndsProduceTurnsSoFarThree(t *testing.T) {
	turnTelemetryRequireImpl(t)

	old := activeReporter
	rep := newStateReporter(registry.NewClient("http://127.0.0.1:1"), "bead-turns-3", "proj")
	activeReporter = rep
	defer func() { activeReporter = old }()

	turn := newRPCTurn(false)
	for _, line := range threeTurnEndLines {
		turn.handleLine(line, nil)
	}

	if got := turn.turnCount(); got != 3 {
		// Counting on the turn object itself must also be wired (the close
		// path reads it even when the reporter era has moved on); pinned so
		// GREEN cannot satisfy the test with reporter-only plumbing.
		t.Fatalf("turnCount = %d, want 3 (the rpc session must count its turn_end events)", got)
	}
	if got := waitForTurns(t, rep, 3, reporterInterval); got != 3 {
		t.Fatalf("TurnsSoFar = %d after 3 turn_end events, want 3 within one heartbeat interval (a fake rpc session streaming 3 turn_end events must produce TurnsSoFar=3)", got)
	}

	// The count must ride the same heartbeat report as the token totals:
	// rep2 verification that a usage-bearing turn_end still updates
	// TokensSoFar alongside the turn count.
	rep.mu.Lock()
	tok := rep.rep.TokensSoFar
	rep.mu.Unlock()
	if tok <= 0 {
		t.Fatalf("TokensSoFar = %d, want >0 — turn forwarding must not clobber token forwarding (TurnsSoFar reports alongside TokensSoFar)", tok)
	}
}

// AC1 (negative): a session without any turn_end event must report zero turns.
func TestTurnTelemetry_NoTurnEndsStayZero(t *testing.T) {
	turnTelemetryRequireImpl(t)

	old := activeReporter
	rep := newStateReporter(registry.NewClient("http://127.0.0.1:1"), "bead-turns-0", "proj")
	activeReporter = rep
	defer func() { activeReporter = old }()

	turn := newRPCTurn(false)
	turn.handleLine(`{"type":"turn_start"}`, nil)
	turn.handleLine(`{"type":"message","message":{"role":"text"}}`, nil)
	if got := sessionTurns(); got != 0 {
		t.Fatalf("sessionTurns = %d, want 0 (a session with no turn_end event must not fabricate turns)", got)
	}
}

// AC2 (RED): the close/complete path emits typed worker_turns with
// {bead, turns, phase, outcome} on a successful worker exit.
func TestTurnTelemetry_CloseSuccessEmitsWorkerTurnsEvent(t *testing.T) {
	turnTelemetryRequireImpl(t)

	home := t.TempDir()
	emitWorkerTurnsEvent(home, "proj-turns", "hydra-tt01", 7, "green", "success")

	line := readWorkerTurnsEvent(t, home)
	if line.Type != workerTurnsEventType {
		t.Fatalf("type = %q, want %q", line.Type, workerTurnsEventType)
	}
	if line.Bead != "hydra-tt01" || line.Project != "proj-turns" {
		t.Fatalf("bead=%q project=%q, want hydra-tt01/proj-turns", line.Bead, line.Project)
	}
	if line.Fields["turns"] != 7 {
		t.Fatalf("turns = %v, want 7", line.Fields["turns"])
	}
	if line.Fields["phase"] != "green" {
		t.Fatalf("phase = %v, want green", line.Fields["phase"])
	}
	if line.Fields["outcome"] != "success" {
		t.Fatalf("outcome = %v, want success", line.Fields["outcome"])
	}
}

// AC2 (RED): the failure exit emits the same typed event with outcome failure.
func TestTurnTelemetry_CloseFailureEmitsWorkerTurnsEvent(t *testing.T) {
	turnTelemetryRequireImpl(t)

	home := t.TempDir()
	emitWorkerTurnsEvent(home, "proj-turns", "hydra-tt02", 2, "red", "failure")

	line := readWorkerTurnsEvent(t, home)
	if line.Fields["outcome"] != "failure" {
		t.Fatalf("outcome = %v, want failure — the close path must emit worker_turns on every worker exit (success and failure)", line.Fields["outcome"])
	}
}

// AC2 (phase derivation): the phase is derived from the bead title prefix.
func TestTurnTelemetry_DeriveBeadPhase(t *testing.T) {
	turnTelemetryRequireImpl(t)

	cases := []struct{ title, want string }{
		{"GREEN: Implement Turn telemetry", "green"},
		{"RED: Write tests for Turn telemetry", "red"},
		{"REFACTOR: Optimize Turn telemetry", "refactor"},
		{"SYNTAX: Check Turn telemetry", "syntax"},
		{"feat: plain bead title", "implement"},
	}
	for _, tc := range cases {
		if got := deriveBeadPhase(tc.title); got != tc.want {
			t.Errorf("deriveBeadPhase(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

// AC2 (wiring shape, enforce-gated guardrail): the close path must reference
// the turn-telemetry emitter at its exit sites — pinned source-level so GREEN
// cannot leave the event emission unwired.
func TestTurnTelemetry_CloseReferencesTurnsEmitter(t *testing.T) {
	turnTelemetryRequireImpl(t)

	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "emitWorkerTurnsEvent(") {
		t.Errorf("cmd/worker/main.go close path does not call emitWorkerTurnsEvent — worker_turns must be emitted on every worker exit")
	}
}

// --- helpers ---

type turnsEventLine struct {
	Time    string         `json:"time"`
	Type    string         `json:"type"`
	Project string         `json:"project"`
	Bead    string         `json:"bead"`
	Fields  map[string]any `json:"fields"`
}

func readWorkerTurnsEvent(t *testing.T, hydraHome string) turnsEventLine {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(hydraHome, "events"))
	if err != nil {
		t.Fatalf("read events dir: %v", err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(hydraHome, "events", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if raw == "" {
				continue
			}
			var line turnsEventLine
			if err := json.Unmarshal([]byte(raw), &line); err != nil {
				t.Fatalf("parse event line: %v", err)
			}
			if line.Type == workerTurnsEventType {
				return line
			}
		}
	}
	t.Fatalf("no %s event found in %s/events", workerTurnsEventType, hydraHome)
	return turnsEventLine{}
}

// compile-time anchor: the events contract rides the central stream shape.
var _ = types.BeadID("")
