# red handoff

## What changed (files + one line each)
- internal/core/epic_close_guard.go (new): `epicCloseGuard` (refusal map, 10m TTL) + `Overseer.epicHasOpenDependent(epicID)` — scans every project's bead stores (single: project store; multi: per-repo stores) for a non-terminal bead depending on the epic; any listing error counts as "has open dependents" (fail-closed).
- internal/core/overseer.go: `persistClosedEpics` now runs under `dagMu` and, per candidate epic: skips if recently refused; refuses the store close (log + typed `epic_close_refused` event) when `epicHasOpenDependent` is true; unresolvable project path records a refusal as before (still a refusal, never a close). New field `epicCloseGuard`.
- internal/core/epic_close_guard_test.go (new): 5 tests — open store child refuses close despite vacuous DAG, clean store allows close, list error refuses, unresolvable path refuses, refusal throttle within TTL.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run TestPersistClosedEpics -count=1` — PASS (5 tests).

## Open edges for the next phase
- The DAG-level vacuous-truth remains: `CloseReadyEpics` still proposes closes over an incomplete DAG each rebuild; the guard refuses persistence, so the store never takes the bad decision, but the epic stays open in memory and is re-proposed every build (throttled scans, 10m). If DAG-side fix wanted later: include blocked/edge-failed children as ghost nodes in dag_builder.
- Verified via live `br list` experiments that br 0.7.0 does NOT filter blocked/dep-hidden beads from `br list` default output — invisibility was a build-side skip (per-repo ListBeads failure path in dag_builder), not a br filter.
- fmsfe beads were purged from the live store before this bead ran; root-cause evidence for which exact skip hid the children is inferred, not directly reproducible.
