# red handoff (Same-fingerprint verification classifier)

## What changed (files + one line each)
- internal/core/bead_closer_fingerprint_test.go (new): 5 tests pinning the CloseGate rejection-fingerprint contract — first occurrence stores exactly `gate-fp-<first8>` (no event), repeat on the same bead emits one `verification_substantive` event `{bead, fingerprint, count:2}` without duplicating the label, a different fingerprint gets its own label and stays silent (per-fingerprint dedup), nil beadsClient is a no-op, and `getVerificationFingerprint` is deterministic/8-hex and sensitive to bead_id + verification command + reason.
- CHANGELOG.md: Unreleased/Added entry recording the test coverage for the FR-006 classifier.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestTrackVerificationFingerprint|TestGetVerificationFingerprint' -count=1` — PASS (ok, exit 0).
- `go vet ./internal/core/` — PASS (clean).
- `go build ./...` — PASS (BUILD_OK).
- `gofmt -l internal/core/bead_closer_fingerprint_test.go` — clean.
- Note: the environment's command-output interceptor masks plain `go test` output; the test binary compile is clean and the run is green, but the daemon gate is authoritative for the full-suite result.

## Open edges for the next phase
- Feature implementation (getVerificationFingerprint / trackVerificationFingerprint in internal/core/bead_closer.go) is already on the worktree from the prior strand; these tests pin its behavior for GREEN/REFACTOR to build on, not replace.
- pan.go's `verification_substantive` guard sentence (FR-006 line) is already present; no further edit expected for this bead.
- Integration should decide whether the uncommitted FR-006 + these tests ride the same PR as the rpc/token strand (task-2cu6) or a separate one.
