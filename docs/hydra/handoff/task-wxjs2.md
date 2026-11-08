# green handoff

## What changed (files + one line each)
- internal/core/coverage_packet.go: GREEN implementation replacing the inert RED stub — FromMeasurements consumes `all_files` into the summary row and derives `metric_N` names; parsePercent parses int/float/percent-suffix cells (unparseable → NaN); BelowBarCount counts files with any metric below the bar (NaN cells breach); WorstFilePerMetric maps each metric index to the lowest-value file; SetBar/default-bar fallback; matched flag; nil/empty zero-value safe; splitRow returns nil for empty rows.
- CHANGELOG.md: Added entry under Unreleased for the GREEN coverage-packet implementation.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestCoveragePacket_' -count=1 -v` — PASS (10 tests, exit 0, no FAIL lines).
- `go build ./...` — PASS (exit 0).
- `go vet ./internal/core/` — PASS (clean).
- Coverage (go test -coverprofile on the narrowed run): 100.0% statements for all coverage_packet.go functions (NewCoveragePacket, FromMeasurements, splitRow, nan, isNaN, parsePercent, effectiveBar, BelowBarCount, WorstFilePerMetric, SetBar, IsMatched, SetMatched).

## Open edges for the next phase
- None. Green phase criteria met; no wiring into gate_junk/publisher decision events was required by the pinned tests (wiring is a separate concern if the chain defines one).
