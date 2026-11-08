# refactor handoff

## What changed (files + one line each)
- internal/core/quarantine_unverifiable.go: new `scarLabels(labels) (hasFailed, hasScar bool)` predicate replaces three duplicated `closedFailedLabel`/`closedFailedUnverifiableLabel` label-scan loops (`sweepUnverifiableStreaks`, `revalidateTargetFor`, `memberScarPresent`). Naming + dedupe only — behavior byte-identical.
- CHANGELOG.md: Unreleased/Changed entry.

## Verification (commands run + pass/fail)
- BEFORE: `go test ./internal/core/ -run 'Unverifiable' -count=1 -v` — PASS 6/6 (`TestUnverifiableStreak_OnlyNoCommandIncrements`, `_ThirdNoCommandStampsAndEmits`, `TestUnverifiable_ThreeTicksStampAndWhoSkipsMember`, `_StreakSurvivesRestartViaRegistry`, `_SweepResetsStreakWhenLabelGone`, `_OperatorRemovalResetsAndReEnables`), ok 0.801s.
- AFTER: same command — PASS 6/6, ok. `gofmt -l internal/core/quarantine_unverifiable.go` — clean. `go vet ./internal/core/` — clean.
- Regression: `go test ./internal/core/ -run 'Quarantine|Reconcile' -count=1` — ok (4.368s).
- No benchmarks exist for this path; none added.

## Open edges for the next phase
- None for this chain. Green-rerun stamp blocks in `revalidateMemberLocked` (classified vs legacy path) remain visually similar but distinct (seam set differs); not worth a shared helper without a behavior-preserving seam indirection.
