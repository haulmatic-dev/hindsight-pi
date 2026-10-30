# red handoff

Bead: hydra-74mf — RED: Write tests for prd-lint coverage-threshold semantics rule
Phase: red

## What changed

- `tools/prd-lint/main_test.go` — appended two RED contract tests + helpers (no production code touched):
  - `TestCoverageThresholdLint_GlobThresholdCriterionWithoutMarkerWarns`: fixture PRD (built from `minimalValidPRD()` plus an `FR-COV` block whose Acceptance Criteria name a coverage threshold over a file glob — ``coverage >= 80% for glob `tools/**` ``) and no exemption marker must produce a `coverage-threshold` warning; the warning must point at block id `FR-COV`, carry a non-zero line number and a non-empty message.
  - `TestCoverageThresholdLint_NonCoverageCriteriaUnaffected`: regression fixture without coverage mentions lints clean; the same fixture with a `<!-- coverage-threshold: exempt -->` marker also lints clean (marker suppresses the warning).
  - Helper `isCoverageThresholdWarning` matches the rule id OR message containing `coverage-threshold` (case-insensitive), so GREEN may place the text in either field.
  - Gate constant `coverageThresholdEnforceEnv` = `PRDLINT_COVERAGE_THRESHOLD_ENFORCE`; `requireCoverageThresholdEnforced` skips loudly by default so `go test ./...` stays green for the daemon gate (established codebase RED pattern: `STALE_FILING_ENFORCE`, `MIRROR_HEALTH_ENFORCE`, `BEAD_EVIDENCE_ENFORCE`).
- `CHANGELOG.md` — Added entry under `## [Unreleased]`.

The rule itself (R10-class coverage-threshold semantics in `tools/prd-lint/main.go`) is intentionally NOT implemented — that is GREEN's job. Existing rules R0–R9 untouched; no second parser added; tests reuse `lintContent` and `minimalValidPRD()`.

## Verification

- `go vet -C tools/prd-lint ./...` → exits 0 (VET_OK). `gofmt -l tools/prd-lint/` → no output.
- Default run `go test -C tools/prd-lint ./ -count=1 -run 'TestCoverageThreshold' -v`: both tests `--- SKIP` loudly with the enforce hint; package `ok` (0 failures). VERIFIED.
- Enforced run `PRDLINT_COVERAGE_THRESHOLD_ENFORCE=1 go test ... -run 'TestCoverageThreshold' -v`: `--- FAIL: TestCoverageThresholdLint_GlobThresholdCriterionWithoutMarkerWarns` with verbatim message `expected the glob coverage-threshold criterion without an exemption marker to produce a coverage-threshold warning, got a clean pass` — RED fails for the missing rule, not a compile error. VERIFIED.
- Coverage of new test code against current sources: `coverage: 59.1% of statements` (enforced run, `-cover`). Post-GREEN the rule implementation will be exercised by these same tests.

Known pre-existing failure on clean HEAD (not introduced here, left untouched per smallest-change doctrine): `TestCreatePrdProducerContract` — `main_test.go:359: precedence term "**tdd_chain**" appears out of order`. Confirmed via `git stash` rerun.

## Open edges

- GREEN implementer: add the rule in `tools/prd-lint/main.go` (suggest `R10-coverage-threshold` id to continue numbering R0–R9); `isCoverageThresholdWarning` accepts the text in Rule or Message. Warning must fire only for blocks with a coverage-threshold-on-glob criterion lacking the `coverage-threshold: exempt` marker (marker placement: anywhere in the block's section, fixture uses a line above the heading).
- The regression/marker test passes vacuously pre-GREEN (nothing emits the warning yet); it becomes meaningful only after GREEN. This is inherent to regression guards and documented here.
- `TestCreatePrdProducerContract` precedence-order failure is pre-existing on `github/development` HEAD (commit e3250d53); needs its own bead if it blocks later gates.
- Daemon test wrapper prints a non-stock `Test Results:` banner; the stock `ok`/`FAIL` lines are authoritative (known measurement trap).
