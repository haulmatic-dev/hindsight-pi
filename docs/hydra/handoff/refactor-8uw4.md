# refactor handoff (hydra-8uw4)

## What changed (files + one line each)
- internal/core/bead_closer.go: `trackVerificationFingerprint` label-membership loop replaced with the existing `hasBeadLabel` helper; the rejection-path fingerprint source selection (first stderr line vs classification reason) extracted into `verificationFingerprintSource(reason, stderr)`. Behavior identical.
- CHANGELOG.md: Unreleased/Changed entry.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit): `go test ./internal/core/ -run 'TestTrackVerificationFingerprint|TestGetVerificationFingerprint' -count=1` — ok (5 fingerprint tests, feature strand from prior chain).
- AFTER: same command — ok (0.041s). `gofmt -l internal/core/bead_closer.go` — clean. `go vet ./internal/core/` — clean (earlier run).

## Open edges for the next phase
- `getVerificationFingerprint` still does `verificationCommandFor(bead)` with the bool discarded; fine (empty command degrades to reason-only fingerprint, by design).
- No benchmarks exist for the fingerprint path; no perf check run.
