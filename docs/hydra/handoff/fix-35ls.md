# fix handoff (fix-35ls)

## What changed (files + one line each)
- internal/core/bead_closer.go: `checkTargetScope` targetless fallthrough now rejects the close when mode is "block" (`non-allowlisted changes on target-less bead: <paths>`); warn mode keeps the advisory log; context-pack anchor enforcement unchanged.
- internal/core/bead_closer_verifiedclose_test.go: `TestVerifyBeforeClose_TargetLessImplementationWarns` moved to `warn` mode (still logs, does not reject); new `TestVerifyBeforeClose_TargetLessImplementationBlocks` asserts `block` mode rejects with the fail-closed reason and violation path.
- CHANGELOG.md: Fixed entry under Unreleased for the fail-closed targetless gate.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS (silent).
- `gofmt -l internal/core/bead_closer.go internal/core/bead_closer_verifiedclose_test.go` — clean.
- `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run 'TestVerifyBeforeClose|TestScopeGate' -count=1` — PASS (ok 2.972s).

## Open edges for the next phase
- Non-implementation bead types (epic, verification, integration) still return nil early on targetless changes — unchanged by design; only types in `beadTypeShouldDeclareTargets` are gated.
- If operator feedback shows block-mode rejects are too aggressive for structural changes (e.g. README under non-docs prefix), the allowlist in `isAllowlistedPath` is the single tuning knob.
