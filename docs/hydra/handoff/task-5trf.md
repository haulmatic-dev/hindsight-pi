# refactor handoff (task-5trf, bead hydra-8vds)

## What changed (files + one line each)
- tools/prd-lint/main.go: R10 coverage-threshold check extracted from the `checkBlock` if-chain into named predicate `coverageThreshold(b reqBlock) (Violation, bool)`; exemption-line match factored into `isExemptMarker(text)`; doc comments added on `reqBlock.covExempt` and the pending-marker-binding semantics in `parseBlocks`. Behavior, rule ID, message, exit codes byte-identical.
- tools/prd-lint/main_test.go: stale RED skip-gate scaffolding removed — `coverageThresholdEnforceEnv` / `requireCoverageThresholdEnforced` deleted; `TestCoverageThresholdLint_*` now assert unconditionally with a contract-describing comment (rule was GREENed by hydra-o589).
- CHANGELOG.md: Unreleased/Changed entry for the refactor + gate removal.

## Verification (commands run + pass/fail)
- BEFORE: ungated run — both coverage-threshold tests SKIP loudly; gated run — PASS 2/2.
- AFTER: `go test ./... -run 'TestCoverageThresholdLint' -v` — PASS 2/2 (ok 0.006s); gated rerun also PASS.
- `go build ./...` — BUILD_OK (rc=0). `go vet .` (in tools/prd-lint) — clean. `gofmt -l tools/prd-lint/` — empty.
- Full `go test ./tools/prd-lint` — 1 pre-existing FAIL (`TestCreatePrdProducerContract: precedence term "**tdd_chain**\ appears out of order"`, SKILL.md §3.1a doc/test drift) — present BEFORE the refactor, unrelated to coverage-threshold; recorded with `cm add` per REFACTOR discipline, not fixed here.

## Open edges for the next phase
- Fix the pre-existing TestCreatePrdProducerContract SKILL.md precedence-wording drift (recorded via cm add).
