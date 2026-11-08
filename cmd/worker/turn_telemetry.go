package main

import (
	"encoding/json"
	"log"
	"strings"
	"time"
)

// Turn telemetry (FR-004): the worker counts the agent turns (rpc turn_end
// events) a live Pi session emits, forwards the count to the heartbeat
// reporter alongside TokensSoFar, and the close path emits one typed
// `worker_turns` event {bead, turns, phase, outcome} per worker exit (both
// success and failure) to the central event stream.
const (
	// GREEN bead hydra-kzlu: wiring complete — rpcTurn counts turn_end +
	// forwards, stateReporter.SetTurns, close path emits worker_turns on
	// success AND failure.
	turnTelemetryImplemented = true
	workerTurnsEventType     = "worker_turns"
)

// workerTurnsRecord mirrors the daemon-side core.Event JSONL contract
// ({"time","type","project","bead","fields"}) so the central events stream
// parses worker-emitted records identically.
type workerTurnsRecord struct {
	Time    string         `json:"time"`
	Type    string         `json:"type"`
	Project string         `json:"project,omitempty"`
	Bead    string         `json:"bead,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// turnCount returns the number of agent turns (turn_end events) observed so
// far in this rpc session.
func (t *rpcTurn) turnCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.turns
}

// forwardTurns pushes the session's turn count to the heartbeat reporter so
// the registry shows turns_so_far alongside tokens_so_far.
func (t *rpcTurn) forwardTurns() {
	if t == nil || activeReporter == nil {
		return
	}
	activeReporter.SetTurns(int64(t.turnCount()))
}

// deriveBeadPhase derives the TDD-chain phase reported in the worker_turns
// event from the bead title prefix (RED:/GREEN:/REFACTOR:/SYNTAX: markers use
// the phase name; anything else is "implement").
func deriveBeadPhase(title string) string {
	for _, p := range []string{"RED:", "GREEN:", "REFACTOR:", "SYNTAX:"} {
		if strings.HasPrefix(title, p) {
			return strings.ToLower(strings.TrimSuffix(p, ":"))
		}
	}
	return "implement"
}

// sessionTurns reads the turn count of the active heartbeat reporter (0 when
// no reporter is live). The close path uses it because the rpcTurn object is
// out of scope there — the reporter is the process-global holder of the count.
func sessionTurns() int {
	if activeReporter == nil {
		return 0
	}
	activeReporter.mu.Lock()
	defer activeReporter.mu.Unlock()
	return int(activeReporter.rep.TurnsSoFar)
}

// emitWorkerTurnsEvent writes the typed worker_turns event {bead, turns,
// phase, outcome} to the central event stream on worker exit. Fail-open: a
// missing HYDRA_HOME or write error logs and continues — the event stream
// observes the system and must never break execution.
func emitWorkerTurnsEvent(hydraHome, project, beadID string, turns int, phase, outcome string) {
	if hydraHome == "" {
		return
	}
	rec := workerTurnsRecord{
		Time:    time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		Type:    workerTurnsEventType,
		Project: project,
		Bead:    beadID,
		Fields: map[string]any{
			"turns":   turns,
			"phase":   phase,
			"outcome": outcome,
		},
	}
	line, err := json.Marshal(rec)
	if err != nil {
		log.Printf("worker_turns: marshal failed: %v", err)
		return
	}
	if err := appendEventLine(hydraHome, line); err != nil {
		log.Printf("worker_turns: append failed: %v", err)
	}
}
