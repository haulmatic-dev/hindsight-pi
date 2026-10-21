# discovery handoff (hydra-2cu6)

## What changed (files + one line each)
- docs/discovery/rpc-token-reporting-blindspot-2cu6.md: discovery — rpc-mode token reporting is exit-only (`reportTurnTokens` called once after `turn.Ended()`, cmd/worker/main.go:1810), so mid-session heartbeats carry tokens_so_far=0 and `detectZeroTokenStalls` (process_hygiene.go:95) flags actively-streaming workers for `kill-stalled-worker` (PID 2353988 class). Pins GREEN fix shape + RED test spec + Charon classifier refinement; resolves 4 ambiguities.
- CHANGELOG.md: Documentation entry under Unreleased.
- No production code change — this is the discovery bead of the FR-003 chain; RED/GREEN/Charon refinement are the next phases' obligations.

## Verified findings (code inspection, this worktree @ 23f01dca)
- `cmd/worker/main.go:1810` — `reportTurnTokens(s)` only inside `if turn != nil && turn.Ended()` branch. VERIFIED.
- `cmd/worker/pi_rpc_turn.go:95` `addUsage` + `:363` call site — usage folded per-message into cumulative counters, `currentStats()` builds snapshot, but never forwarded during the session. VERIFIED.
- `cmd/worker/reporter.go:194` `SetTokenBreakdown` — writes `r.rep.TokensSoFar`; next 30s `beat()` flushes. Registration is fail-open. VERIFIED.
- `internal/core/process_hygiene.go:80,95` — stall = working + `TokensSoFar==0` + age>15m; no rpc-activity guard today. VERIFIED.
- `internal/core/charon.go:458-459` — stall count surfaced to Pan with `repair:kill-stalled-worker`; no "rpc-active but token-silent" path exists yet. VERIFIED.
- `internal/core/worker_lifecycle.go:343` `zeroTokenFailure` — kill misclassified as infra, bead left open for respawn. VERIFIED.
- CASS rule b-msiq2rki-5bh523 corroborates the flush requirement (heartbeat-only reporting loses final values).
- `hindsight_search` — not connected (skip rule applied). `graphify query` — graph present, FR-003 node + rpcTurn + zero-token test nodes found; no pre-existing GREEN implementation.

## Open edges for the next phase (RED then GREEN)
- RED: extend `cmd/worker/token_reporter_test.go` (exit-path regression already there — extend, don't fork): fake rpc session emitting `turn_end` usage events (input>0 or output>0) through the reporter path must show `TokensSoFar > 0` within one heartbeat interval; must fail on current main.
- GREEN: forward `currentStats()` to `reportTurnTokens` at the addUsage site (~pi_rpc_turn.go:363) when the usage block is countable; absolute totals, no throttling.
- Charon: add rpc-activity signal (preferred: `LastRpcActivity` on registry WorkerReport, set on every usage/notification event) + 5m window + one-line `[Charon] rpc-active but token-silent: <bead>` dedup before `detectZeroTokenStalls` flags; exclude from kill-stalled-worker surfacing.
- Risk: WorkerReport field addition touches `internal/registry` API — check readers at overseer.go:537 and token_usage_rollup.go.
- Hung-worker catch-all remains `shouldKillAsStuck` (heartbeat-based) — fail-open by design; the new rpc guard must not disable it.
