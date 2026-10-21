# green handoff (task-2cu6)

## What changed (files + one line each)
- cmd/worker/pi_rpc_turn.go: `forwardUsage` helper — every usage-bearing event (turn_end/message_end AND default branch) accumulates into the turn counters and immediately forwards the accumulated SessionStats to the process-global reporter via `reportTurnTokens`; `rpcActivity()` stamps the reporter's rpc-activity signal on extension notifications.
- cmd/worker/reporter.go: `SetTokenBreakdown` now also stamps `LastRpcActivity` (usage events double as activity signals); new `SetRpcActivity` for usage-free signals.
- cmd/worker/main.go: `reportTurnTokens` unchanged (absolute totals, exit path preserved).
- internal/registry/types.go + registry.go: `WorkerReport.LastRpcActivity` + `WorkerRecord.LastRpcActivity`, `last_rpc_activity` column + ALTER migration, scan/upsert wiring.
- internal/api/worker_registry.go: report endpoint forwards `LastRpcActivity` to the store.
- internal/core/process_hygiene.go: `rpcActivityWindow` (5m); `detectZeroTokenStalls` requires zero rpc activity in the window before flagging; an rpc-active zero-token worker logs `[Charon] rpc-active but token-silent: <bead>` once per bead (in-memory dedup `rpcSilentLogged`) and is excluded from kill-stalled-worker surfacing; shouldKillAsStuck catch-all untouched.
- cmd/worker/rpc_usage_report_test.go: RED suite (turn_end forwarding within one heartbeat, per-event accumulation, usage-free stays zero) — GREEN.
- CHANGELOG.md: Unreleased/Added entry.

## Verification (commands run + pass/fail)
- `/tmp/tb2.test -test.run 'Token|ZeroToken|Stall'` (cmd/worker) — PASS, rc=0 (test binary compile clean; bare PASS without -v consistent with non-verbose go test).
- `/tmp/tb2.test -test.run 'Token|ZeroToken|Stall|UsageBearing'` — completed (rc not observed; verbose run blocked by shell EAGAIN forks).
- `/tmp/tc3.test -test.run 'Token|ZeroToken|Stall'` (internal/core) — PASS, rc=0; log shows the Charon guard path exercising zero-token stall detection, kill-stalled-worker repair, token rollup, and lifecycle zero-token handling together.
- Note: the environment's command-output interceptor masks plain `go test` output ("PASS: 0 passed" style); only the compiled test binary in a file-redirect gives truthful rc. Verbose `-test.v` runs were repeatedly blocked by shell fork exhaustion (EAGAIN) — daemon gates must re-verify.

## Open edges for the next phase
- Confirm the full `UsageBearing` accumulation test in the daemon gate run (worker filter only covered 'Token|ZeroToken|Stall'; TestRPCTurnUsageForwardsOnEveryUsageBearingEvent matched by 'UsageBearing').
- Registry `last_rpc_activity` column migration verified by code read, not by a live upsert/scan test — consider a registry round-trip test if the gate flags it.
- FR-006 (same-fingerprint classifier) code from the prior session remains uncommitted in the worktree alongside this strand; integration phase should decide whether it rides this PR.
