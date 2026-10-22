# red handoff (cpc8)

## What changed (files + one line each)
- internal/core/pan_stale_filing.go (new): RED-phase inert stub — `staleFilingReasonCleared` ("stale pan filing: premise cleared (cohort empty, spawns healthy)"), `staleFilingMinAge` 48h, `staleFilingSpawnWindow` 24h, `branchProtectionReasonRe` (`branch protection|required approval|permissions|GH013|rules URL`), probe hooks `staleFilingIsOld` / `staleFilingCohortNamed` / `staleFilingCohortBlocked` / `staleFilingLastSpawnOK` (inert defaults), `staleFilingSweepImplemented=false`, inert `Overseer.sweepStalePanFilings(now)`.
- internal/core/pan_stale_filing_test.go (new): 6 contract tests — eligible in_progress filing closes with exact cleared reason (first tick) and eligible open filing closes on second tick (open+in_progress coverage + per-tick rate-limit reset); branch-protection reason never auto-closed; cohort still blocked → not closed; no recent spawn success → not closed; filing younger than 48h → not closed; two eligible filings in one project per tick → exactly 1 close + typed `pan_filing_stale_closed` event with fields {cohort_named, cohort_now, last_spawn_ok} and bead/project top-level.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'StaleFiling|PanHealth' -count=1` — PASS, exit 0 (suite skips the 2 close-path tests loudly pre-GREEN via `staleFilingRequireImpl`; guardrail tests pass and would catch over-closing).
- `STALE_FILING_ENFORCE=1 go test ./internal/core/ -run 'StaleFiling' -count=1` — FAIL exactly 2/6, right reason: `first tick: exactly one auto-close (rate limit), got 0: []` and `one auto-close per project per tick, got 0` — missing sweep behavior, not a compile error. RED proven.
- `go build ./internal/core/` — PASS. `go vet ./internal/core/` — PASS (clean). `gofmt -l` on touched files — clean.
- Placeholder grep (TODO/FIXME/Placeholder) — clean.
- Coverage (`-coverprofile` over StaleFiling run): `sweepStalePanFilings 0.0%` (RED stub, inert by design); GREEN must move this to full coverage.
- Commit aed96639 (3 files, +287), verified via `git show --stat`.

## Contract GREEN must implement
- Real `Overseer.sweepStalePanFilings(now)` body: per project via `o.GetProjects()`, list beads, evaluate pan-filed non-terminal (open + in_progress) filings where (a) older than 48h, (b) named cohort has zero still-blocked members, (c) a successful spawn in last 24h → `o.beadsClient.CloseBead(projectPath, "", beadID, staleFilingReasonCleared)`, ≤1 per project per tick, emit `pan_filing_stale_closed` (EmitRepo) `{bead, project, cohort_named, cohort_now, last_spawn_ok}`; never close filings matching `branchProtectionReasonRe`; probe errors fail open.
- Flip `staleFilingSweepImplemented = true`; replace the four inert probe hooks with real deterministic probes (default `staleFilingLastSpawnOK` may stay injectable-backed but must be correct in production).

## Open edges for the next phase
- `staleFilingLastSpawnOK` real probe: no existing spawn-success tracker found (rg: no `spawn_success` events) — GREEN may need to scan central events for a spawn/worker-started event type or add the tracker.
- `staleFilingCohortNamed` real probe must parse the cohort name from the filing's description/reason text deterministically.
- Wiring: no Start/Stop loop pinned in RED — mirror IntegrationExhaustedSweeper/PostCloseSweeper cadence if a periodic tick is wanted.
