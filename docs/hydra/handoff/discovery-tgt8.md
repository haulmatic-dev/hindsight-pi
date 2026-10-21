# discovery handoff (tgt8)

## What changed (files + one line each)
- internal/core/spawn_preflight.go: added `brListProbe` (runs `br list --json --limit 1` in projectPath, 5s timeout) and `offendingStorePathsDetail` (up to 3 foreign-uid paths under `.beads`, format `.beads/<name>: uid <n>`); `spawnPreflightStoreOwnership` now: probe exit 0 → `logx.Debugf` only, no `spawn_warn_store_ownership` event; failing probe → warn log + event whose detail names the offending paths.
- internal/core/spawn_preflight_store_ownership_test.go: removed unused `os/exec` import; `newOwnershipTestOverseer` takes `*testing.T`; 3 pre-existing RED tests restored (probe-success silent, probe-failure path detail, cap-at-3) and pass.
- CHANGELOG.md: Unreleased/Fixed entry for FR-005.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'StoreOwnership' -count=1` — PASS 5/5 (incl. TestStoreOwnershipPreflight_ProbeSuccessIsSilent, _ProbeFailureWarnsWithPaths, _CapsOffendingPathsAtThree).
- `go build ./...` — PASS. `gofmt -l` on touched files — clean.

## Open edges for the next phase
- Green-phase wire-in: `spawnPreflightStoreOwnership` already calls the probe; no further wiring expected.
- `brListProbe` uses PATH-resolved `br`; production daemon PATH must include the br binary (it already does for other br probes).
