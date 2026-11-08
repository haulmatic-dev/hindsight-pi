# syntax handoff (task-ppd3)

## What changed (files + one line each)
- None (verification-only SYNTAX pass). R10 coverage-threshold rule in `tools/prd-lint/main.go` already conforms — implemented (hydra-o589 GREEN), refactored to `coverageThreshold` predicate + `isExemptMarker` (hydra-5trf), tests ungated (RED skip gate removed).
- CHANGELOG.md: Documentation entry under Unreleased recording this verification round.

## Verification (commands run + pass/fail)
- `go build -C tools/prd-lint ./...` — PASS (BUILD_OK, exit 0).
- `go vet .` (tools/prd-lint) — PASS (VET_OK, clean).
- `gofmt -l tools/prd-lint/` — clean (empty).
- `go test -C tools/prd-lint ./... -count=1 -run 'TestCoverageThresholdLint' -v` — PASS 2/2:
  `--- PASS: TestCoverageThresholdLint_GlobThresholdCriterionWithoutMarkerWarns`
  `--- PASS: TestCoverageThresholdLint_NonCoverageCriteriaUnaffected`.
- Placeholder grep (TODO/FIXME/Placeholder) over main.go + main_test.go — 0 hits.
- `grep -c 'coverage-threshold' tools/prd-lint/main.go` — 8 (rule + markers present).
- Full `go test -C tools/prd-lint ./... -count=1` — 1 FAIL: `TestCreatePrdProducerContract` — `main_test.go:359: precedence term "**tdd_chain**" appears out of order`. PRE-EXISTING on origin/development (confirmed by hydra-5trf handoff: SKILL.md §3.1a doc/test drift, unrelated to coverage-threshold; recorded via cm add at that bead). Out of scope for this SYNTAX bead.

## Open edges for the next phase
- `TestCreatePrdProducerContract` needs the §3.1a SKILL.md reword (or the test updated to the landed wording) — separate bead, affects `.agents/skills/create-prd/SKILL.md` + `tools/prd-lint/main_test.go`.
