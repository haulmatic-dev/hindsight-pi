# green handoff

## What changed (files + one line each)
- internal/core/pan_stale_filing.go: real stale-filing sweep — `sweepStalePanFilings` (interval-gated) + `staleFilingCandidates` + `sweepStalePanFilingsForProject`: pan-filed open/in_progress filings older than 48h whose named cohort has zero still-blocked members and whose project recorded ≥1 successful spawn in last 24h auto-close with reason `stale pan filing: premise cleared (cohort empty, spawns healthy)`; one auto-close per project per tick; typed `pan_filing_stale_closed` event `{bead, project, cohort_named, cohort_now, last_spawn_ok}`; branch-protection reason regex guardrail (never auto-closed, left for human triage).
- internal/core/overseer.go: probe wiring (`wireStaleFilingProbes`), `noteSpawnSuccessFiling` recording per-project successful spawn from `WorkerSpawner.Spawn`, throttle field `lastStaleFilingSweep`, sweep call in tick.
- internal/core/pan_stale_filing_test.go: 6 contract tests — eligible in_progress closes first tick, open closes second tick (rate-limit reset), branch-protection never closed, cohort still blocked not closed, no recent spawn not closed, young filing not closed, rate limit + typed event shape.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'StaleFiling|PanHealth' -count=1` — PASS, exit 0. All 6 tests: TestStaleFilingSweep_ClosesEligibleInProgressAndOpenFilings, _BranchProtectionReasonNeverClosed, _CohortStillBlockedNotClosed, _NoRecentSpawnSuccessNotClosed, _YoungFilingNotClosed, _RateLimitAndTypedEvent. `ok github.com/haulmatic-dev/hydra/internal/core 0.014s`.
- `go build ./...` — PASS (rc=0).
- `go vet ./internal/core/` — PASS, clean.
- Coverage (`go tool cover -func` on stale-filing run): sweepStalePanFilings 100.0%, staleFilingCandidates 100.0%, sweepStalePanFilingsForProject 77.8%, isPanFiledBead 75.0%. Production-only helpers (noteSpawnSuccessFiling, lastSpawnSuccessFor, hasCohortMember real store branch, filepathBaseForSpawnProbe, pathBase, wireStaleFilingProbes) sit at 0% — test harness intercepts the probe seams (staleFilingListBeads / staleFilingEligible hooks), so the store-backed branches count as open edges, not gaps in the sweep logic itself.
- Placeholder grep (`// TODO|FIXME|stub|Placeholder` in pan_stale_filing.go + _test.go) — 0 matches.

## Open edges for the next phase
- 0%-covered helpers above need real-store integration tests if the harness ever stops mirroring the store (e.g. an integration-e2e bead).
- `noteSpawnSuccessFiling` ledger is in-process; a daemon restart forgets spawn history until the next successful spawn — acceptable (premises take 48h to age anyway).
- Overseer tick wiring call sites: overseer.go ~132-134 (throttle field), ~571 (wireStaleFilingProbes), ~977-978 (interval-gated sweep) — refactor/relocate if the tick shape changes.
