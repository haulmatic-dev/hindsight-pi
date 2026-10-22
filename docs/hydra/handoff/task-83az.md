# refactor handoff (83az)

## What changed (files + one line each)
- cmd/worker/close_hold.go: new `closeWithHold(bead, projectPath, repoName, reason, verdict)` — single close-with-hold entrypoint wrapping `closeHoldFor` + `runCloseHold` + `closeBeadFromWorker`; returns the close error verbatim, nil when close is held (bead intentionally stays open).
- cmd/worker/main.go: both worker close sites (success path + auto-close guard) collapse from duplicated `closeHoldFor && runCloseHold` branches into one `closeWithHold` call each. Log lines consolidated to one WARN naming close-hold-or-close-failed.
- CHANGELOG.md: Unreleased/Refactor entry for the dedupe.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS ("[OK] Build successful").
- `go test ./cmd/worker/ -run 'CloseHold' -count=1` — PASS (6 tests).
- `go vet ./cmd/worker/` — clean (empty output).
- `gofmt -l cmd/worker/close_hold.go cmd/worker/main.go` — clean (empty).
- Commit verified via `git show --stat HEAD` (3 files, +22/−12).

## Re-verified (final session, 2026-10-22, wt-hydra-8uw4)
- `go build ./...` — PASS (empty output, exit 0) on commit 378c39af.
- `go test ./cmd/worker/ -run 'CloseHold' -count=1` — PASS (`ok github.com/haulmatic-dev/hydra/cmd/worker 0.006s`).
- `git status --short` — clean; refactor landed as 378c39af on hydra/wt-hydra-8uw4.

## Open edges for the next phase
- Behavior identical to pre-refactor: held close still emits `worker_close_held` {bead, hold_count, verdict_summary, forced}; forced at hold_count==2 (maxCloseHolds); nil/malformed verdict fails open (close proceeds as before).
- No benchmarks exist for the close-hold path; none run (no hot loop).
- `persistCloseHoldLabel` still uses br CLI with fail-open error handling — real br workspace paths remain untested (prev handoff note stands).
