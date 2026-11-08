package registry

import "time"

// WorkerReport is the worker's self-reported state, sent to the daemon on
// every beat. The daemon (single writer) upserts it.
type WorkerReport struct {
	BeadID              string `json:"bead_id"`
	PID                 int    `json:"pid"`
	PGID                int    `json:"pgid"`
	Project             string `json:"project"`
	State               string `json:"state"`
	PiSessionID         string `json:"pi_session_id"`
	ErrStatus           int    `json:"err_status"`
	ErrType             string `json:"err_type"`
	ErrMessage          string `json:"err_message"`
	TokensSoFar         int64  `json:"tokens_so_far"`
	TokensFresh         int64  `json:"tokens_fresh"`
	TokensCacheRead     int64  `json:"tokens_cache_read"`
	TokensCacheCreation int64  `json:"tokens_cache_creation"`
	TokensOutput        int64  `json:"tokens_output"`
	// TurnsSoFar counts the agent turns (rpc turn_end events) the worker has
	// observed in this session. Heartbeat telemetry only — Green wire-in is
	// cmd/worker/turn_telemetry.go; the column is not persisted yet.
	TurnsSoFar  int64     `json:"turns_so_far"`
	ParkSince   time.Time `json:"park_since"`
	ParkWakeAt  time.Time `json:"park_wake_at"`
	ParkReason  string    `json:"park_reason"`
	NudgeCount  int       `json:"nudge_count"`
	WorktreeKey string    `json:"worktree_key"`
	// LastRpcActivity is the worker's most recent rpc-stream event (a turn
	// usage event or an extension notification). Charon uses it to keep an
	// active-but-zero-token worker out of the zero-token stall classifier.
	LastRpcActivity time.Time `json:"last_rpc_activity"`
}

// CmdCloseSatisfied tells Pan to close a bead whose acceptance criteria are
// already met on the working branch, without spawning or continuing a worker.
const CmdCloseSatisfied CommandKind = "close_satisfied"

// CompletionReport marks a worker terminal.
type CompletionReport struct {
	BeadID   string `json:"bead_id"`
	ExitCode int    `json:"exit_code"`
}
