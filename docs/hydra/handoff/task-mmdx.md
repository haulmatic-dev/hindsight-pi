# green handoff

## What changed (files + one line each)
- internal/core/verification_runner.go (new): `VerificationRunner` — argv-only exec (no `sh -c` anywhere; injection strings delivered verbatim), credential env vars stripped from child (inherited + ExtraEnv), process-group timeout kill with `WaitDelay` (grandchild-held stdout pipes unblocked), vacuous zero-output exit-0 rejected (`Passed=false, Vacuous=true`), provenance (Argv/ExitCode/StartedAt/FinishedAt/TimedOut), `RunWithEvents` emitting `verification_passed`/`verification_failed` with provenance fields.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- RED tests cherry-picked from bf7a056 (branch hydra/wt-hydra-bfp7) onto this worktree.
- `go test ./internal/core/ -run TestVerificationRunner -count=1 -v` — PASS 8/8 (InjectiveArgNotShell, SecondArgvElementUntouchedByShell, CredentialEnvNotInChild, TimeoutKillsProcess 3.05s, RejectsVacuousZeroOutput, RealOutputPasses, ProvenanceCaptured, EventsOnPassAndFail).
- `go vet ./internal/core/` — clean.
- `gofmt -w` on new file — clean.
- Coverage (new implementation code): NewVerificationRunner 100%, Run 100%, execute 80.8%, childEnv 87.5%, isCredentialEntry 75%, RunWithEvents 81.2%, excerpt 75%, isTimeoutKill 75%.

## Open edges for the next phase
- TEST SEMANTIC NOTE: `TestVerificationRunner_CredentialEnvNotInChild` pins that exit-1-with-non-empty-output PASSES (printenv-class probes report via stdout presence; /bin/false = exit 1 + no output still fails). Pass rule documented inline in Run; flag if the parent requirement intends strict exit-0-only semantics.
- execute() uncovered lines: empty-command guard and start-failure (-1) paths.
- spec.Dir field exists in contract but unused by tests; not implemented beyond the struct field in the test file — runner currently ignores it.
