# REFACTOR handoff (hydra-8uw4) — rpc-mode token reporting / zero-token blind spot

## What changed (files + one line each)
- internal/core/process_hygiene.go: dedupe + readability only, no behavior change — the triplicated lazy `j.hygiene` init collapsed into `ensureHygiene()` (the `KillStalledWorker` copy had drifted, omitting the `rpcSilentLogged` map init); the once-only `[Charon] rpc-active but token-silent` log block moved into `processHygiene.logRpcSilent(beadID)`; the stall loop and the emit/log pass each compute a worker's age once (`age := now.Sub(r.StartedAt)`) instead of re-deriving it three times per stall.
- CHANGELOG.md: Unreleased/Changed entry for the dedupe.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'ZeroToken|Stall|Token' -count=1` — ok (0.075s), all Charon zero-token/rogue-pi hygiene tests pass post-refactor.
- `go test ./cmd/worker/ ./internal/core/ -run 'Token|ZeroToken|Stall' -count=1` — ok both packages (0.057s / 0.068s) — the bead's AC gate passes.
- `gofmt -l internal/core/process_hygiene.go` — clean.
- RED-before behavior: untouched; the FR-003 forwarding path (`cmd/worker/pi_rpc_turn.go: forwardUsage`) and the stall classifier logic were not modified, only their hygiene-state bookkeeping factored out.

## Open edges for the next phase
- `internal/core/worker_registry.go` (the bead's listed target file) does not exist in this tree; the actual FR-003 surfaces are `internal/core/process_hygiene.go`, `cmd/worker/reporter.go` (`SetTokenBreakdown`/`SetRpcActivity`), and `cmd/worker/pi_rpc_turn.go` (`forwardUsage`). Bead metadata may need a `br update` of target_files (same disposition as test-6lmz/test-82sj).
- No benchmarks exist for the stall-scan path; no perf regression check run (loop is O(workers) with one subtraction saved per iteration).
- Unrelated: worktree contained pre-existing gofmt drift in `cmd/worker/{chain_handoff_test,close_hold,execution_report_test,main_test,turn_economy_test}.go` and `internal/core/{gate_runner_test,quarantine_reconciler,worker_completion_test}.go`; reverted to keep this diff scoped (smallest-change discipline).
