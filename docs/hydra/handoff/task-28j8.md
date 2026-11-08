# refactor handoff

## What changed (files + one line each)
- internal/core/spawn_preflight.go: `offendingStorePathsDetail` now takes the daemon uid as an explicit parameter (no duplicated `daemonUID()` seam call) and its entry loop separates the stat/ownership check from the record-append step; `storeOwnershipDetail(projectPath, storeUID, daemon)` threads the daemon uid through and is the single detail builder called by `spawnPreflightStoreOwnership`. Behavior identical.
- CHANGELOG.md: updated the Refactor entry under `[Unreleased]` to describe the parameter-threading refactor.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'StoreOwnership' -count=1` — PASS (ok, 0.014s).
- `gofmt -l internal/core/spawn_preflight.go` — clean.

## Open edges for the next phase
- None.
