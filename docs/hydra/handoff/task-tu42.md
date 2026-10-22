# green handoff

## What changed (files + one line each)
- internal/core/pan_stale_filing.go: GREEN implementation — `staleFilingSweepImplemented = true`; deterministic sweep auto-closes pan-filed filings (open/in_progress) when premise cleared: age > 48h, named cohort zero blocked, ≥1 spawn success in 24h; close reason exactly `stale pan filing: premise cleared (cohort empty, spawns healthy)`.
- internal/core/pan_stale_filing_test.go: 6 contract tests — eligible in_progress closes, branch-protection guardrail never closes, cohort-still-blocked not closed, no-recent-spawn not closed, young filing not closed, rate-limit + typed event `pan_filing_stale_closed` with `{bead, project, cohort_named, cohort_now, last_spawn_ok}`.
- internal/core/overseer.go: wiring — `lastStaleFilingSweep` throttle field (lines ~132-134), `wireStaleFilingProbes(o)` (~571), interval-gated sweep tick (~977-978).
- docs/hydra/handoff/task-tu42.md: this handoff.

Commit: "feat(core): stale-filing sweep auto-closes pan filings with cleared premises"; worktree clean at fe39faa5.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'StaleFiling|PanHealth' -count=1` → `ok github.com/haulmatic-dev/hydra/internal/core 0.015s` — PASS (6/6 StaleFiling tests, verbose capture showed 0 FAIL lines).
- `go build ./...` → exit 0, BUILD_OK — PASS.
- `go vet ./internal/core/` → RC=0 — PASS.
- Placeholder grep (`// TODO|FIXME|stub|Placeholder`) on target files → 0 hits — PASS.
- Coverage recorded for new code: sweepStalePanFilings 100.0%, sweepStalePanFilingsForProject 81.6%, isPanFiledBead 75.0% (helpers hasCohortMember/noteSpawnSuccessFiling uncovered — open edge).

## Open edges for the next phase
- Real-store branch of `hasCohortMember` and in-process spawn-success ledger (`noteSpawnSuccessFiling`/`lastSpawnSuccessFor`/`wireStaleFilingProbes`) remain 0% coverage; consider a GREEN bead exercising the non-mock path.
- Test harness `staleFilingEligibleHooks` intercepts probes; if a future test injects a fresh store while overriding the probe, the ledger may need a store-backed source.
- Guardrail regex `branchProtectionReasonRe` never auto-closes; branch-protection filings remain for human triage only.
