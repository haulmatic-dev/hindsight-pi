# refactor handoff (hydra-k991)

## What changed (files + one line each)
- internal/core/bead_closer.go: `verifyBeforeClose` (207-line monolith) decomposed into 9 named helpers — `skipIfTerminal` (terminal short-circuit, fail-closed on GetBead error), `verifyTDDChain` (TDD gate, error = not blocking), `resolveEvidenceRepo` (worktree/repo selection + main path), `closeIntegrationBase` (deterministic base, empty in main-checkout mode), `collectCloseEvidence` (diff + session/committed override + leak union), `unionMainCheckoutLeaks` (2jjz8), `acceptAlreadyIntegrated` (5k33p dispatcher), `chainBranchIntegrated` (branch ancestry + per-target merge-base), `worktreeHEADIntegratedLocally` (hydra-7k1 commit-level check). Incident-context comments preserved on the owning helper. Integration base now resolved once per close (was twice); drops one duplicate `integration_base_mismatch` log line — only observable behavior delta.
- internal/core/bead_closer_hard_gates.go (earlier commit 235210a, same bead): `emitGateEvent` helper unifies 3 duplicate `EmitRepo` blocks; `closeDepOpenEvent` const.
- CHANGELOG.md: two Changed entries under Unreleased.
- cm add (both runs): hard-gate `gateExecutableAcceptance` never sets `cmd.Dir` (runs in daemon cwd, untested). Recorded, not fixed.

## Verification (commands run + pass/fail)
- Narrow close/verify set before AND after edits: `go test ./internal/core/ -run 'TestVerifyBeforeClose|TestVerifiedClose|TestEmptyDiff|TestCloseBead|TestCloseGate|TestClose|TestBeadCloser|TestTerminalGuard' -count=1` — PASS.
- Full package: `go test ./internal/core/ -count=1` — ok 366.572s (0 FAIL lines).
- `go vet ./internal/core/` — clean. `gofmt -l internal/core/bead_closer.go` — clean.
- Benchmarks: `go test ./internal/core/ -run XXX -bench 'BenchmarkScopeViolations|BenchmarkTargetlessWarningPaths'` — PASS (1113 ns/op / 321.8 ns/op; no close-gate benches exist, none added).

## Open edges for the next phase
- BUG (CASS-recorded, unfixed): gate 1 verification command executes in daemon cwd, not worktree — needs `cmd.Dir = dir` + a `pwd`-pinning test.
- Scripted span-replacement used for the big verifyBeforeClose edit (exact-match edit failed on transcription whitespace); replaced span verified by span size (9427 chars), compile, vet, and test run.
- Pre-existing in-flight modifications to stranded_work.go / verification_runner.go (+ their CHANGELOG entries) belong to concurrent strands, untouched.
