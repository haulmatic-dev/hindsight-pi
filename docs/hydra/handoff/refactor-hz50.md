# refactor handoff (hydra-hz50)

## What changed (files + one line each)
- internal/core/verification_runner.go: decomposed the sandboxed verification runner into named single-responsibility helpers — `finalizeProvenance` (provenance stamping), `classifyResult` (vacuous/pass verdict rules, doc comments), `applyRunError` (timeout/exit/start-failure mapping, unified error handling), `verificationEvent` (typed event + Detail construction extracted from RunWithEvents); removed the unused `prov` parameter from `execute`. Behavior identical; no public API change.
- CHANGELOG.md: Changed entry under Unreleased.

## Verification (commands run + pass/fail)
- Baseline BEFORE edits: `go test ./internal/core/ -run TestVerificationRunner -count=1` — PASS (ok).
- AFTER: `go test ./internal/core/ -run TestVerificationRunner -count=1 -v` — PASS 8/8 (InjectiveArgNotShell, SecondArgvElementUntouchedByShell, CredentialEnvNotInChild, TimeoutKillsProcess 3.04s, RejectsVacuousZeroOutput, RealOutputPasses, ProvenanceCaptured, EventsOnPassAndFail).
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l internal/core/verification_runner.go` — clean.

## Open edges for the next phase
- The validation-gap claim ("RED bead has no test files") is stale: verification_runner_test.go exists on this branch and passes — same disposition as test-6lmz/test-izp2/test-udlg.
- `childEnv` still loops twice over credential scrubbing (inherited + extra); a future pass could fold it into one `appendUncredentialedFrom` helper. Deferred as smallest-change discipline.
- No benchmarks exist for the runner.
