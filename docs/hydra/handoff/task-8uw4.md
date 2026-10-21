# REFACTOR handoff (hydra-8uw4)

## What changed (files + one line each)
- internal/core/bead_closer.go: `trackVerificationFingerprint` deduped — label-membership loop replaced with `hasBeadLabel`; fingerprint source selection extracted into `verificationFingerprintSource(reason, stderr)`. No behavior change.
- CHANGELOG.md: Unreleased/Changed entry describing the refactor.
- docs/hydra/handoff/refactor-8uw4.md: this handoff (template contract).

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestTrackVerificationFingerprint|TestGetVerificationFingerprint' -count=1` — ok (PASS, 0.041s pre-commit; 0.022s re-verified post-commit).
- `gofmt -l internal/core/bead_closer.go` — clean.
- Commit landed as `1e881a2a` ("refactor: dedupe fingerprint label check and extract source selection (hydra-8uw4)").

## Open edges for the next phase
- No benchmarks exist for the fingerprint path; no perf regression check needed or run.
- `getVerificationFingerprint` discards `verificationCommandFor`'s bool — intentional (empty cmd degrades to reason-only fingerprint).
