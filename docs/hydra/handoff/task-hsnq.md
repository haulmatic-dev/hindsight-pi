# refactor handoff (hydra-hsnq)

## What changed (files + one line each)
- internal/core/process_hygiene.go: dedupe + readability only, no behavior change — triplicated lazy `j.hygiene` init collapsed into `ensureHygiene()` (the `KillStalledWorker` copy had drifted, omitting the `rpcSilentLogged` map); once-only `[Charon] rpc-active but token-silent` log block moved into `processHygiene.logRpcSilent(beadID)`; stall loop + emit/log pass each compute a worker's age once (`age := now.Sub(r.StartedAt)`) instead of three times.
- CHANGELOG.md: Unreleased/Changed entry for the dedupe (landed with the refactor commit).
- Note: the refactor content was landed and integrated during the earlier strand of this chain (originally committed as d65a8658, now part of HEAD ancestry after integration rebase); this bead's final pass verified the landed state on current HEAD 69ff4ac3.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS (no output, rc=0).
- `go test ./cmd/worker/ ./internal/core/ -run 'Token|ZeroToken|Stall' -count=1` — PASS (ok cmd/worker 0.060s, ok internal/core 0.083s).
- `grep -c "ensureHygiene|logRpcSilent" internal/core/process_hygiene.go` — 6 hits; refactor present at HEAD.
- `gofmt -l internal/core/process_hygiene.go` — clean.
- RED-before behavior untouched: FR-003 forwarding (`cmd/worker/pi_rpc_turn.go: forwardUsage`) and the stall classifier logic unchanged — only hygiene-state bookkeeping factored out.

## Open edges for the next phase
- `internal/core/worker_registry.go` (the bead's listed target file) does not exist in this tree; actual FR-003 surfaces are `internal/core/process_hygiene.go`, `cmd/worker/reporter.go` (`SetTokenBreakdown`/`SetRpcActivity`), `cmd/worker/pi_rpc_turn.go` (`forwardUsage`). Same bead-metadata disposition as test-6lmz/test-82sj (fix is `br update target_files`, not code).
- No benchmarks exist for the stall-scan path; no perf regression check run (O(workers) loop, one subtraction saved per iteration).
