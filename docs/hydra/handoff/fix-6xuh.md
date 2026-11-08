# fix handoff (6xuh)

## What changed (files + one line each)
- internal/core/pan_stale_filing.go: committed stale-filing sweep —
  eligibility: pan-filed (or code-change proposal routed through filePanBead)
  beads matching `staleFilingCohortRe` with a `blocked on <cohort>` reason;
  48h min age; one cohort-free member left alive (`hasCohortMember` false) OR
  a successful spawn within 24h; branch-protection reasons never closed;
  typed `pan_filing_closed_stale` event with the cleared-premise reason;
  fail-open (nil-sink safe).
- internal/core/overseer.go: `lastStaleFilingSweep` field + 30m sweep
  interval check at hook-site line 977.
- CHANGELOG.md: Unreleased/Added entry for the sweep.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS (exit 0).
- `go test ./internal/core/ -run 'StaleFiling' -count=1` — PASS (6 tests:
  ClosesEligibleInProgressAndOpenFilings, BranchProtectionReasonNeverClosed,
  CohortStillBlockedNotClosed, NoRecentSpawnSuccessNotClosed,
  YoungFilingNotClosed, RateLimitAndTypedEvent).
- `git show 2629484c --stat` — pan_stale_filing.go +5/−2, CHANGELOG +1.

## Open edges for the next phase
- Untracked `beads-mirror-audit` dir in worktree root — unrelated artifact,
  not touched by this bead.
- Design constraints (docs/discovery/stale-filing-sweep-context.md §design
  constraints) — all 6 satisfied: fail-open, no re-fire (dedupe/cap interplay
  intact), premise probes reused via pan_health.go family, in-progress
  filings respected, typed events emitted.
