# refactor handoff

## What changed (files + one line each)
- internal/core/validation_service.go: behavior-preserving dedupe/readability refactor — duplicated TDD-validator construction (RunTDDValidation + TDDChainPasses) extracted into `newTDDValidator()`; duplicated retry-round/cap-default logic (RunTDDValidation + RunPRDValidation, both had inline `GetValidationRetryCount` + `MaxRounds<=0→3` + exhaustion compare) extracted into `validationRounds()` with `defaultValidationRounds = 3` const; the twice-repeated within-run fix-bead dedup append extracted into `recordCreatedFix()`; the symmetric RED/GREEN dependency role check in `findTDDChainBeads` extracted into `isTDDRole(bead, keyword, beadType)`. All log lines, dedup titles, label names, priorities, and close-path behavior byte-identical. No changes needed in config.go / bead_closer.go (close-path wiring was already single-sourced through `shouldRunTDDValidation`; config knobs already have doc comments and `TargetScopeMode` normalization).
- CHANGELOG.md: Changed entry under Unreleased.

## Verification (commands run + pass/fail)
- Baseline BEFORE (pre-edit): `go test ./internal/core/ -run 'TestTDDVerdict_|TestValidationMode_|TestRunTDDValidation' -count=1 -v` — PASS, 5 `--- PASS` (TestRunTDDValidation_SkipsWhenDisabled, _NoRedGreenBeads, _CreatesFixBeadOnGap, _ExhaustedRetries, _DedupsOpenFixBeads), `ok github.com/haulmatic-dev/hydra/internal/core`. Note: the parent criteria name `TestTDDVerdict_*`/`TestValidationMode_*` files (validation_verdict_test.go, validation_mode_test.go) which do not exist on this branch — the chain's TDD phase landed its contract tests under the `TestRunTDDValidation_*` names (overseer_chain_validation_test.go + validation_dedup_test.go). The verification command matches those 5 and no `[no tests to run]`.
- AFTER (final): same verification command — PASS, same 5 `--- PASS`, `ok ... 0.054s`, no `[no tests to run]`, 0 FAIL.
- `go test ./internal/core/ -run 'TestRunPRDValidation|TestPRDValidator' -count=1` — PASS (ok; PRD dedup/fix-bead paths unaffected).
- `go vet ./...` — PASS (clean, rc=0).
- `gofmt -l internal/core/validation_service.go` — clean.
- `go build ./internal/core/` — PASS.
- Gate budget: verification command executed 2× total (baseline + final), within the ≤3 limit. Full suite NOT run (daemon gate owns it). No benchmarks exist for validation_service paths; none added.

## Open edges for the next phase
- The TDD gap loop (RunTDDValidation) and PRD gap loop (RunPRDValidation) remain structurally similar but deliberately NOT merged: they differ in fixType selection (test/fix), dependencies (refactor bead vs none), fix-title source (bead title vs gap title), label prefix (tdd-/prd-), and re-validation bead creation (PRD only). Merging would couple two contracts for marginal savings.
- `RunPRDValidation` still opens with a `[PRDValidation] DEBUG START` log line (pre-existing); candidate for removal in a future behavior-change bead, not this one.
- `validationRounds` returns round even when exhausted; callers keep their distinct exhaustion log wording ("proceeding" vs "marking complete") by design — evidence preserved.
