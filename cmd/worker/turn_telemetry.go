package main

// TODO(RED): worker turn telemetry.
//
// This file exists so the package compiles while the RED contract is being
// pinned (cmd/worker/turn_telemetry_test.go). Today the worker never counts
// the rpc turn_end events a live Pi session emits (verified: zero turn_end
// observability — TokensSoFar is forwarded per usage-bearing event, turns are
// not forwarded anywhere and the close path emits nothing about turns).
//
// GREEN must implement, flipping turnTelemetryImplemented to true:
//   - rpcTurn counts each observed turn_end event (agent turns) and forwards
//     the count to the heartbeat reporter alongside TokensSoFar.
//   - stateReporter exposes the count so the close path can read it back.
//   - the close/complete path emits one typed `worker_turns` event
//     {bead, turns, phase, outcome} per worker exit, success AND failure.
const (
	turnTelemetryImplemented = false
	workerTurnsEventType     = "worker_turns"
)

// turnCount returns the number of agent turns (turn_end events) observed so
// far in this rpc session. RED stub: inert, always 0.
func (t *rpcTurn) turnCount() int {
	return 0
}

// forwardTurns pushes the session's turn count to the heartbeat reporter.
// RED stub: inert no-op.
func (t *rpcTurn) forwardTurns() {}

// deriveBeadPhase derives the TDD-chain phase reported in the worker_turns
// event from the bead title prefix (RED:/GREEN:/REFACTOR:/SYNTAX: markers use
// the phase name; anything else is "implement"). RED stub: inert, returns "".
func deriveBeadPhase(title string) string {
	return ""
}

// sessionTurns reads the turn count of the active heartbeat reporter (0 when
// no reporter is live). RED stub: inert, always 0.
func sessionTurns() int {
	return 0
}

// emitWorkerTurnsEvent writes the typed worker_turns event to the central
// event stream on worker exit. RED stub: inert no-op.
func emitWorkerTurnsEvent(hydraHome, project, beadID string, turns int, phase, outcome string) {}
