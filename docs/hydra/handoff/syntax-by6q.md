# syntax handoff (by6q)

## What changed (files + one line each)
- internal/core/pan_mirror_health.go: `mirrorResyncImplemented` flipped to true — the FR-2/FR-3/FR-4 sweep body (audit loop, guarded heal via BrUpdater, FR-4 `fileMirrorDrift` filing through `beadsClient.CreateBead` + `mirror_drift_needs_human` event, exactly-once 24h ledger) was fully implemented but gated off by the RED flag; heal eligibility tightened to require `rec.Class == mirroraudit.ClassDBAhead` (a jsonl_ahead record with a terminal db status is NOT healable — snapping the jsonl to the db status would erase the divergence it did not cause; it files for a human instead).
- internal/core/pan_mirror_health_test.go: `mirrorDriftFixture` now sets `Counts: {JSONLAhead: 1}` (the shared `Report.Drift()` predicate counts by class; without it every drift fixture read as clean — root cause of 13 RED failures); new `applyMirrorHooks()` test seam that re-applies post-`mirrorEnv` hook overrides (the staging struct is not live); it is now called in the 6 tests that override hooks after `mirrorEnv` (mtime quiescence, live-worker skip, typed events, both FR-4 filing tests); `TestMirrorHealthSweep_TypedEvents` first sweep now drives a drift fixture instead of an empty report.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `MIRROR_HEALTH_ENFORCE=1 go test ./internal/core/ -run 'MirrorHealth' -count=1` — BEFORE the fix: FAIL (13 failures: `user-owned divergent store must file exactly one pan bead, got 0 calls`, `non-terminal drift must never heal via BrUpdater, got 1 heal invocations`, `filing sink must have been attempted exactly once despite the failure, got 0`, …); AFTER: **ok, exit 0** (all 25 MirrorHealth tests pass under enforce).
- `go test ./internal/core/ -run 'MirrorHealth' -count=1` — PASS (default gate, no env needed).
- `go test ./internal/core/ ./internal/mirroraudit/ -run 'TestMirrorHealthSweep|TestAudit' -count=1` — PASS both packages.
- `go build ./...` — PASS (exit 0).
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l internal/core/pan_mirror_health*.go` — clean.

## Open edges for the next phase
- `mirrorHealEnv` seeding guard hooks (`isDaemonOwned`/`writeProbe`) are package vars set inside mirrorHealEnv before tests override them — those tests still work because the guard hooks are consulted live (not staged); no change needed.
- FR-1 SLO-script/docs tests (mirror_health_docs_test.go) remain gated on `MIRROR_HEALTH_DOCS_ENFORCE` and did not run in this pass — they belong to the FR-5 chain, not this bead.
- `mirrorDriftFiledLedger` is in-memory (restart re-files once); acceptable per the already-recorded design decision.
