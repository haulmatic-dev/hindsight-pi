# green handoff

## What changed (files + one line each)
- internal/core/bead_closer.go: rpc-mode token reporting integration (FR-003) — usage totals forwarded through the close path instead of staying exit-only.
- internal/core/gate_close.go: aligned close gate with forwarded token state.
- internal/core/spawn_preflight.go: preflight accounts for the reported token state so zero-token workers are not misclassified.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./cmd/worker/ ./internal/core/ -run 'Token|ZeroToken|Stall' -count=1` — PASS (ok cmd/worker 0.073s, ok internal/core 0.191s).
- `go build ./...` — PASS (0 units compiled, exit 0).
- `gofmt -l cmd/worker internal/core` — clean.
- `go vet ./cmd/worker/ ./internal/core/` — clean.
- `git show --stat HEAD` — 4 files changed, +37/−24, commit 64921b25. PASS.

## Open edges for the next phase
- Registry `TokensSoFar`/`LastRpcActivity` now populated mid-run; Charon's zero-token classifier (internal/core/process_hygiene.go:100) only flags when no rpc activity in the last 5m window — operator should verify no false `kill-stalled-worker` filings after deploy.
- Per-event forwarding (cmd/worker/pi_rpc_turn.go:196, :296) and rpc-activity guard verified by `cmd/worker/rpc_usage_report_test.go` tests; if further tuning needed, the `forwardUsage` accumulation is the single touchpoint.
