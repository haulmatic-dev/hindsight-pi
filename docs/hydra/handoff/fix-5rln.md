# fix handoff (fix-5rln)

## What changed (files + one line each)
- internal/core/postclose_sweep.go: scan() implemented — previously an inert RED stub (tests failed at runtime with `expected bead reopened to open, got status ""`). Now: closed beads with `metadata["test_command"]`, ≤24h old, are re-verified in the project checkout; a failing run is re-run once (pass → `postclose-flake` label, no reopen, no comment); double failure → reopen (`StatusOpen`) + `postclose-reopened` label + comment carrying BOTH failing-run outputs (via `postCloseCommentBody`); failure after a prior reopen adds `needs-human` instead of a second reopen; listing errors fail-open. `Start()` now launches a real 1h tick loop (+ one startup scan); `Stop()` unchanged.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- BEFORE: `go test -c ./internal/core/ && /tmp/core.test -test.run TestPostCloseSweep -test.v` — 3 FAIL / 2 PASS / (skip-old + nil-safe pass; reopen, flake, second-reopen, integration FAIL).
- AFTER: same command — 6/6 PASS (`--- PASS` count = 6).
- `gofmt -l internal/core/postclose_sweep.go` — clean.
- `go vet ./internal/core/` — clean.
- Commit d1b6f9d verified via `git show --stat` (2 files, +111/−18).

## Open edges for the next phase
- Sweeper is not yet constructed/wired into overseer/service_starter (RED contract only pinned the type + scan behavior; no wiring test). If live activation wanted, follow IntegrationExhaustedSweeper wiring in overseer.go + service_starter.go.
- Second failing-run error string (err2) is captured but not yet rendered into the comment beyond Run 2 output block — outputs include run-2 stdout/stderr text; explicit `Run 2 error:` line exists.
- No benchmarks exist for this functionality (consistent with prior handoffs).
