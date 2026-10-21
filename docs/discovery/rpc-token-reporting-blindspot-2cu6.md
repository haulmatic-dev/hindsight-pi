# Discovery: rpc-mode token reporting closes the zero-token blind spot (FR-003)

Bead: hydra-2cu6 · phase: discovery · 2026-10-21

## The blind spot (verified against code)

**rpc-mode workers report tokens only at session exit.** The registry heartbeats
every 30s (`cmd/worker/reporter.go:32` `reporterInterval`), but during an
in-flight rpc session every heartbeat carries `tokens_so_far=0` because
`reportTurnTokens` is called exactly once, after the session ends
(`cmd/worker/main.go:1810`, inside the `turn.Ended()` branch).

- Token data exists in-process during the session: `rpcTurn.addUsage`
  (`cmd/worker/pi_rpc_turn.go:95`) folds every usage-bearing event (top-level
  `event["usage"]` or nested `event["message"]["usage"]`, `extractUsage`
  line ~125) into cumulative counters, and `currentStats()` (line 165)
  builds the SessionStats snapshot. The data is collected per-message but
  never surfaced until exit.
- Consequence (reproduces PID 2353988 class): a worker actively streaming
  turns for >15m looks identical to a dead one — `detectZeroTokenStalls`
  (`internal/core/process_hygiene.go:80,95`: working + `TokensSoFar==0` +
  age>15m) flags it, Pan surfaces `repair:kill-stalled-worker`
  (`internal/core/charon.go:458`), and the healthy worker gets killed
  mid-run. `wl.zeroTokenFailure` (`internal/core/worker_lifecycle.go:343`)
  then misclassifies the kill as infra, leaving the bead open for a respawn
  that burns the same trajectory again.
- CASS institutional rule corroborates (b-msiq2rki-5bh523): registry state
  updates that only mutate the local WorkerReport must be flushed with
  `beat()` — heartbeat-only reporting loses final values such as
  `tokens_so_far`. Here the inverse applies: the value is never written into
  `r.rep` until exit, so all mid-session heartbeats are zero.

## GREEN fix shape (pinned, no parser duplication)

1. **Reporter path** (`cmd/worker/pi_rpc_turn.go`, addUsage call site ~L363):
   after folding a usage block with at least one countable field >0, forward
   the accumulated `currentStats()` to `reportTurnTokens(...)` immediately
   (absolute totals; `SetTokenBreakdown` already writes `r.rep.TokensSoFar`,
   and the next `beat()` — ≤30s later — flushes it). No second parser; reuse
   `extractUsage` + `currentStats`.
2. **Test (RED)**: fake rpc session that emits `turn_end` events carrying
   usage (input>0 or output>0) through the reporter path; assert
   `TokensSoFar > 0` within one heartbeat interval. Must fail on current
   main (call is exit-only). Home: `cmd/worker/token_reporter_test.go`
   already pins the exit-path regression — extend, don't fork.
3. **Charon classifier refinement** (`internal/core/process_hygiene.go`
   `detectZeroTokenStalls`, surfaced via `charon.go:456-459`): additionally
   require **no rpc activity signal** — extension notices or turn events in
   the last 5m — before flagging. An active-but-zero-token worker logs at
   most one info line `[Charon] rpc-active but token-silent: <bead>` and is
   excluded from `kill-stalled-worker` surfacing (belt-and-braces: even with
   fix 1, a worker inside a long tool call between turns is genuinely
   token-silent but not dead).
   Activity signal source: the notifications counter already exists on
   `rpcTurn` (`notifications map[string]int`, line ~50) but lives in the
   worker, not the registry row. GREEN must either add a lightweight
   rpc-activity timestamp to the WorkerReport (preferred: `LastRpcActivity`,
   set on every usage/notification event, carried by the same heartbeat) or
   reuse `TokensFresh` age — a `LastRpcActivity` field is the precise signal
   the AC names.

## Ambiguities resolved for GREEN

- **"Every usage-bearing event" vs heartbeat cost**: SetTokenBreakdown is an
  in-memory mutex-guarded mutation; forwarding per-event is free, the
  registry write only happens at the 30s beat. No throttling needed.
- **Absolute vs incremental**: counters are cumulative across turn resets
  (`reset()` deliberately preserves them); forward absolute totals, matching
  `reportTurnTokens`' existing contract ("Absolute, not incremental").
- **Zero-usage messages**: `extractUsage` already treats all-zero blocks as
  absent — forwarding only when `Total > 0` satisfies "input>0 or output>0".
- **Charon log-once semantics**: the "at most one info line" per worker
  requires a dedup entry (beadID → logged-at) on the Charon struct, mirroring
  the existing `ToolHealthDedupStore` pattern but in-memory is sufficient
  (workers are short-lived; restart re-logging one line is harmless).

## Verification commands (GREEN gate)

`go test ./cmd/worker/ ./internal/core/ -run 'Token|ZeroToken|Stall' -count=1`

## Risks

- WorkerReport gains a field → `internal/registry` is a shared API; check
  `overseer.go:537` (reads TokensSoFar) and `token_usage_rollup.go` readers
  for nil/zero tolerance.
- The 15m stall threshold + 5m rpc window means a genuinely hung worker with
  rpc events still arriving (e.g. extension notify loop) is now never killed
  by the stall classifier — the hung-heartbeat path (shouldKillAsStuck,
  charon.go ~L478) remains the catch-all. Fail-open, by design.
