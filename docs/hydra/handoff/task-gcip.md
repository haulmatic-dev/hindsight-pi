# refactor handoff (gcip)

## What changed (files + one line each)
- internal/core/bead_claims.go: `matchClaimLine(line) (BeadClaim, bool)` extracted from `parseBeadClaims` (non-claim/empty-assertion skip folded into one predicate); probe-outcome switch in `checkBeadClaims` replaced with `classifyClaimStatus(output, err)` + named constants `claimStatusPass`/`claimStatusFail`/`claimStatusUnknown`. `checkBeadClaims` loop body extracted to `evaluateClaim(beadID, claim) bool` (bool = "spawn allowed by this claim"; hard-fail only false → fail-closed gate explicit); redundant Go-1.25 loop shadow removed. Behavior identical.
- CHANGELOG.md: Refactor entry under Unreleased.
- NOTE: during this bead, an uncommitted +11-line delta on `internal/core/bead_claims_test.go` (from a concurrent strand) was inadvertently discarded when restoring that file after a bad overwrite by this strand; HEAD version + refactor verified green. Re-check the sibling strand's test intent.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'BeadClaims|DriftCheck' -count=1` — PASS (ok, 0.632s).
- `gofmt -l internal/core/bead_claims.go internal/core/bead_claims_test.go` — clean.
- `go vet ./internal/core/` — clean.
- No benchmarks exist for this functionality; no perf check run.

## Open edges for the next phase
- Discarded test-file delta (see NOTE) is the only regression risk.
