# FR-004 Discovery — Turn telemetry in worker close events

Bead: hydra-rinq (discovery, FR-004) · branch hydra/wt-hydra-kzlu · HEAD dcd0d65a

## Verified current state (code inspection, this worktree)

- **Zero turn_end observability in the worker runtime today.** `cmd/worker/pi_rpc_turn.go:296`
  handles `turn_end`/`message_end` for usage forwarding (FR-003 `forwardUsage`) and error
  surfacing, but never counts turns. `main.go`'s `turnCount` (line 1628) is the in-run
  nudge counter (verdict-nudge turns), reset per attempt — not session telemetry and not
  reported anywhere.
- **RED surface already staged on this branch by a sibling strand (untracked files, inert):**
  - `cmd/worker/turn_telemetry.go` — inert stub: `turnTelemetryImplemented=false`,
    `workerTurnsEventType="worker_turns"`, inert `rpcTurn.turnCount()`, `forwardTurns()`,
    `deriveBeadPhase()`, `sessionTurns()`, `emitWorkerTurnsEvent()`. No test file yet
    (`turn_telemetry_test.go` does not exist — RED strand's remaining obligation).
  - `internal/registry/types.go` (modified, uncommitted): `WorkerReport.TurnsSoFar int64`
    (`turns_so_far`) — heartbeat-only telemetry column; intentionally NOT persisted.
  - `tools/turn-report/` (untracked, inert stub, `turnReportImplemented=false`): reads
    `worker_turns` events from `$HYDRA_HOME/events/<YYYY-MM-DD>.jsonl`, groups by phase,
    prints p25/p50/p75/p90/max, emits a suggested `turn_budgets:` yaml block as COMMENT.
- The FR-003 machinery this hooks into is landed and green: per-event `forwardUsage`
  (`pi_rpc_turn.go:196` via `reportTurnTokens`), `stateReporter.SetTokenBreakdown` stamps
  `LastRpcActivity` (`reporter.go:196-206`), `WaitForTokens` test pattern in
  `rpc_usage_report_test.go`.

## Contract (pinned by the staged stubs — GREEN obligations)

1. **Turn counting**: `rpcTurn` counts each observed `turn_end` event (agent turns,
   independent of usage presence — a turn_end with zero usage still counts). Persist
   across nudges: `reset()` clears end-of-turn state but must NOT clear the turn counter
   (mirror the fresh/cacheRead accumulator semantics, `pi_rpc_turn.go:52-57`).
2. **Heartbeat**: `forwardTurns()` (on every turn_end, same call site as `forwardUsage`)
   stamps a new `stateReporter` mutator next to `SetTokenBreakdown`, so the next 30s
   `beat()` flushes `turns_so_far` to the registry row. Reporter must expose the running
   count for the close path (`sessionTurns()`, 0 when no reporter is live).
3. **Close event**: one typed `worker_turns` event `{bead, turns, phase, outcome}` per
   worker exit — success AND failure. Wiring point: the worker exit paths in
   `cmd/worker/main.go` (around the close-with-hold / `closeBeadFromWorker` site and the
   failure exit at line ~569), fail-open (event write failure never gates the close).
   `deriveBeadPhase` maps title prefixes RED:/GREEN:/REFACTOR:/SYNTAX: → phase name,
   everything else "implement". `outcome` ∈ {completed, failed, held, killed} — match the
   worker's existing exit classification where cheap, fall back to exit-code sign.
4. **RED test first**: fake rpc session streaming 3 `turn_end` events (reuse
   `feedRPCLines` from `rpc_usage_report_test.go`) must produce `TurnsSoFar=3` — must
   fail on current main (today nothing counts turn_end; the staged stub is inert, which
   is exactly the RED-predecessor state).
5. **Gate command**: `go test ./cmd/worker/ ./internal/core/ -run 'Turn' -count=1` exits 0.

## Ambiguities resolved

- **turn_end with no `message.usage`**: counts as a turn. Counting is independent of
  usage accumulation (rate-limited error turns still burn agent turns).
- **Nudged turns**: counted individually (each nudge = one agent turn). The existing
  `main.go turnCount` nudge counter is NOT reused — different lifecycle (per-attempt).
- **`phase` derivation**: from the bead title prefix only (RED:/GREEN:/REFACTOR:/SYNTAX:),
  "implement" otherwise — same vocabulary as `isChainTitle` in
  `internal/core/bead_closer_hard_gates.go`. No chain-graph lookup (fail-open simplicity).
- **TurnsSoFar persistence**: heartbeat-only (`turns_so_far` in WorkerReport JSON);
  no registry column for now. The `worker_turns` close event is the durable record the
  turn-report tool consumes.
- **worker_turns on worker crash/kill**: wired at the ordinary exit paths only
  (fail-open). A SIGKILLed worker emits no event — turn-report percentiles are computed
  from observed exits; acceptable skew.

## GREEN checklist

- flip `turnTelemetryImplemented` and `turnReportImplemented` to true
- `cmd/worker/turn_telemetry_test.go` (the 3-turn RED test) — currently missing; RED strand
  or GREEN must land it
- `tools/turn-report/main_test.go` exists in the sibling strand's plan (not present here)
- one `worker_turns` emission wired at both main.go exit paths
- CHANGELOG entry; `go test ./cmd/worker/ ./internal/core/ -run 'Turn' -count=1`
