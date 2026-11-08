# refactor handoff (task-b8dy, bead hydra-b8dy)

## What changed (files + one line each)
- internal/core/coverage_packet.go: behavior-preserving refactor — new `metricName(i)` helper dedupes the `"metric_" + strconv.Itoa(i)` key construction that appeared in both `FromMeasurements` and `WorstFilePerMetric`; extracted the repeated below-bar/NaN cell check into `isBreachingCell(cell, bar)` and `anyCellBreaches(row, bar)` so `BelowBarCount` reads as one clear predicate per row instead of a nested loop with in-place NaN handling.
- CHANGELOG.md: Added a `## [Unreleased]` "Changed" entry describing the refactor.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestCoveragePacket_' -count=1 -v` — PASS (10/10: FromMeasurements_Nil, _Empty, _PopulatesFields, SplitRow_Variants, ParsePercent_Variants, BelowBarCount, BelowBarCount_CustomBar, WorstFilePerMetric, MatchedFlag, ZeroValueSafety).
- `gofmt -l internal/core/coverage_packet.go` — clean (no output).
- `go vet ./internal/core/` — clean (no output).

## Open edges for the next phase
- No benchmarks exist for the coverage-packet parsing path; perf criteria not applicable.
- `FromMeasurements` still mutates its input map (deletes `"all_files"`); pinned by `TestCoveragePacket_FromMeasurements_PopulatesFields` — record as a follow-up if callers start relying on the map surviving the call, do not change here.
