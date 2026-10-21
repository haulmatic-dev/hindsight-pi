# discovery handoff (FR-006 / task-ktqp)

## What changed (files + one line each)
- internal/core/bead_closer.go: `verificationFingerprintSource` now always returns the classification reason string (e.g. "verification-command-failed") and deliberately ignores the literal stderr — the fingerprint is `sha256(bead_id + verification command + classification reason)`, so same-classification failures with different output collide and repeat as `verification_substantive` instead of fragmenting into distinct `gate-fp-<first8>` labels. Comments updated to match.
- internal/core/bead_closer_fingerprint_test.go: new `TestVerificationFingerprintSource_IgnoresStderr` — different stderr lines yield identical fingerprint source (reason); nil stderr falls back to reason.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ ./internal/pan/ -run 'Fingerprint|VerifiedClose' -count=1` — PASS (RC 0; 11 `--- PASS`, 0 `--- FAIL` in verbose run).
- `go build ./internal/core/ ./internal/pan/` — PASS.
- `go vet ./internal/core/` — PASS.
- `gofmt -l internal/core/` — 3 pre-existing unformatted files (gate_runner_test.go, quarantine_reconciler.go, worker_completion_test.go), none touched by this change; my edited files clean.
- Note: bash tool was persistently failing with `spawn /bin/bash EAGAIN`; verification was executed via a Python-subprocess alternate path (same commands, same cwd).

## Open edges for the next phase
- `verificationFingerprintSource(reason, stderr)` still accepts the stderr variadic for call-site stability; if no caller ever passes stderr, the parameter could be dropped in a later cleanup.
- Pan sweep prompt already carries the "verification_substantive must NOT receive repair:clear-escalation" sentence (landed in the prior FR-006 phase); unchanged here.
