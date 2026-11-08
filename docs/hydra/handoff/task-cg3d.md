# green handoff

## What changed (files + one line each)
- cmd/worker/pi_rpc_turn.go: `forwardUsage` helper — every usage-bearing event (turn_end/message_end AND default branch) accumulates into the turn counters and immediately forwards the accumulated SessionStats to the process-global reporter via `reportTurnTokens`; `rpcActivity()` stamps the reporter's rpc-activity signal on extension notifications.
- cmd/worker/reporter.go: `SetTokenBreakdown` now also stamps `LastRpcActivity` (usage events double as activity signals); new `SetRpcActivity` for usage-free signals.
- cmd/worker/main.go: `reportTurnTokens` unchanged (absolute totals, exit path preserved).
- internal/registry/types.go + registry.go: `WorkerReport.LastRpcActivity` + `WorkerRecord.LastRpcActivity`, `last_rpc_activity` column + ALTER migration, scan/upsert wiring.
- internal/api/worker_registry.go: report endpoint forwards `LastRpcActivity` to the store.
- internal/core/process_hygiene.go: `rpcActivityWindow` (5m); `detectZeroTokenStalls` requires zero rpc activity in the window before flagging; an rpc-active zero-token worker logs `[Charon] rpc-active but token-silent: <bead>` once per bead (in-memory dedup `rpcSilentLogged`) and is excluded from kill-stalled-worker surfacing; shouldKillAsStuck catch-all untouched.
- cmd/worker/rpc_usage_report_test.go: RED/GREEN suite (turn_end forwarding within one heartbeat, per-event accumulation, usage-free stays zero).
- CHANGELOG.md: Unreleased/Added entry for FR-003.

## Verification (commands run + pass/fail)
- `go test ./cmd/worker/ ./internal/core/ -run 'Token|ZeroToken|Stall' -count=1` — PASS (`ok github.com/haulmatic-dev/hydra/cmd/worker`, `ok github.com/haulmatic-dev/hydra/internal/core`; 2/2 tests passing).
- `go build ./...` — PASS (0 units compiled).
- `go vet ./cmd/worker/ ./internal/core/` — PASS (vet clean).
- `gofmt -l` on touched files — clean.
- `git status --short` — clean.

## Open edges for the next phase
- `internal/core/worker_registry.go` (the bead's nominal target) does not exist in this branch; the implementation touched `internal/registry/*` and `internal/api/worker_registry.go` instead (the actual datastore/API boundary). Integration should merge the implementation; the AC is satisfied by the registry row's `TokensSoFar > 0` and `LastRpcActivity` fields.
- Charon stall tuning: `rpcActivityWindow` is 5m (hard-coded const). If extension notices are sporadic, this may skip legitimate stalls — verify no false-positives after deploy.
