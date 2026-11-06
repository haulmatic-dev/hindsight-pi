# green handoff

- **Bead:** hydra-74mf (GREEN phase, task-o589)
- **Phase:** green
- **Files:** `tools/prd-lint/main.go`, `CHANGELOG.md`

## What landed

Coverage-threshold semantics rule for prd-lint (GREEN for the RED tests in
`tools/prd-lint/main_test.go`, `TestCoverageThresholdLint_*`).

- New rule **R10-coverage-threshold** in `checkBlock`: a block whose body
  matches `covThreshRe` (`(?i)coverage\s*>=?\s*\d+\s*%`) and that is not
  covered by an exemption marker yields
  `Violation{Rule: "R10-coverage-threshold", ID: block id, Line: heading line,
  Message: "states a coverage threshold without a `<!-- coverage-threshold:
  exempt -->` marker (coverage-threshold semantics)"}`.
- Exemption markers (`<!-- coverage-threshold: exempt -->`) are detected in
  `parseBlocks` via `covExemptRe` on the immediately-preceding
  non-blank context and carried on `reqBlock.covExempt`; a hooky non-marker
  line clears the pending flag so it cannot leak to an unrelated block.
- New helper `blockText(lines []line) string` joins block bodies for
  whole-block pattern checks.

## Verification (all VERIFIED, run in this session)

- `go build -C tools/prd-lint ./...` — exit 0
- `PRDLINT_COVERAGE_THRESHOLD_ENFORCE=1 go test -C tools/prd-lint ./... -count=1 -v` —
  `TestCoverageThresholdLint_GlobThresholdCriterionWithoutMarkerWarns` PASS,
  `TestCoverageThresholdLint_NonCoverageCriteriaUnaffected` PASS.
  NOTE: `TestCreatePrdProducerContract` fails ("**tdd_chain** appears out of
  order") — pre-existing, fails identically without the new rule; unrelated
  to this bead.
- `go vet -C tools/prd-lint ./...` — clean
- Coverage (enforced run): 80.5% of statements overall; new code
  `parseBlocks` 100%, `blockText` 100%, `checkBlock` 65.2% (exercised paths
  for R10 covered by the two contract tests).
- No `TODO`/`FIXME`/`Placeholder` in `main.go`.
- Tests skip loudly absent `PRDLINT_COVERAGE_THRESHOLD_ENFORCE` (RED gate
  left as-is in `main_test.go`); daemon suite stays green.

## Notes

- `main_test.go` unchanged (RED contract already complete for this bead).
- `min coverage` semantics intentionally simple: any `coverage >= N%` style
  threshold in a block body triggers unless exempted. Glob detection is not
  required by the contract tests.
