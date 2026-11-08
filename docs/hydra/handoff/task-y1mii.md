# Handoff — task-y1mii (bead y1mii)

## Task
RED-phase TDD bead: write failing tests for **"Coverage measurements in decision packets and events"** targeting `internal/core/coverage_packet.go` and `internal/core/coverage_packet_test.go`. Do NOT implement the feature.

## Status: DONE (RED)

## Files changed
- `internal/core/coverage_packet.go` (NEW) — inert stub gated on `COVERAGE_PACKET_ENFORCE=1`.
- `internal/core/coverage_packet_test.go` (NEW) — 10 `TestCoveragePacket_` tests.
- `CHANGELOG.md` — entry under `## [Unreleased]` (Added).

## Contract pinned by tests
- `CoveragePacket{ AllFilesRow []string, FileRows map[string][]string, MetricNames []string, Bar float64, Matched bool }`
- `NewCoveragePacket()` → bar = `DefaultCoverageBar` (80.0), non-nil `FileRows`.
- `FromMeasurements(ms)`:
  - `nil` → `nil` packet.
  - empty → non-nil packet, empty rows.
  - populated → consumes `ms["all_files"]` into `AllFilesRow` (delete from map), rest → `FileRows` via `splitRow`.
- `splitRow(s)` splits on spaces, commas, tabs; empty → nil.
- `parsePercent(s)` parses `"80"`, `"80.5"`, `"80%"`, `" 72.3% "`; unparseable (`""`, `abc`, `50.x%`) → NaN (compare via `got == got` is false).
- `BelowBarCount()` counts files with ≥1 metric strictly `< Bar`; `SetBar(0)` falls back to default bar (80.0); nil receiver → 0.
- `WorstFilePerMetric()` returns `metric_N` → filepath with lowest value (NaN cells ignored); nil receiver → empty map.
- `SetMatched()` / `IsMatched()`; nil receiver safety throughout.

## RED enforcement pattern
`const coveragePacketEnforce = os.Getenv("COVERAGE_PACKET_ENFORCE") == "1"` — default (unset env) keeps `FromMeasurements` returning `nil` and stub methods returning zero values. Tests never set the env var. GREEN removes the stubs and fills in logic; no test edits required.

## Verification (all VERIFIED this session)
- `go vet ./internal/core/` → exit 0 (VET_OK).
- `go build ./internal/core/` → exit 0.
- `go test ./internal/core/ -run 'TestCoveragePacket_' -count=1 -v`:
  - 3 PASS: `FromMeasurements_Nil`, `MatchedFlag`, `ZeroValueSafety`.
  - 7 FAIL (correct RED reason — stub inert): `FromMeasurements_Empty`, `FromMeasurements_PopulatesFields`, `SplitRow_Variants`, `ParsePercent_Variants`, `BelowBarCount`, `BelowBarCount_CustomBar`, `WorstFilePerMetric`.
  - No panics. Earlier panic in `PopulatesFields` (nil deref on stub `nil` packet) fixed by adding a nil guard: `if got == nil { t.Fatal(...) }` before dereferencing.
- Coverage profile (`-coverprofile=/tmp/cov-red.out`): 0.1% of package statements overall; `coverage_packet.go` fn-level: `NewCoveragePacket` 100%, `FromMeasurements` 18.2% (early return branch only — the gate), `splitRow` 100%, `parsePercent` 100%, `BelowBarCount` 100%, `WorstFilePerMetric` 100%, `SetBar` 50% (nil-receiver branch untested), `IsMatched` 100%, `SetMatched` 100%.

## Gotchas for GREEN bead
- `AllFilesRow` is `[]string`, not string/`nil`-able. Use `len() == 0`.
- `parsePercent` returns NaN for unparseable input — never `==` compare with a number; use `x != x` NaN idiom.
- `FromMeasurements` must delete the `all_files` key from the caller's map (mutating input is intentional & asserted).
- Coverage % considers `not-a-pct` cells a breach in `BelowBarCount` (any unparseable cell counts against the file).
- Methods must tolerate nil receivers (`(&CoveragePacket{})` zero-value and nil pointer both tested).
- `SetBar(0)` must revert to `DefaultCoverageBar`.
- GREEN: remove stub bodies, keep the `COVERAGE_PACKET_ENFORCE` gate removed (or invert), wire into `decisionPacketMeasurements` in `internal/core/pan_decision_packet.go`.

## Summary for verdict
RED state achieved: 10 tests pinning the CoveragePacket contract; 7 fail for the right reason against the inert stub; 0 panics; vet/build clean; coverage of the new code measured (18.2% FromMeasurements = gate branch).
