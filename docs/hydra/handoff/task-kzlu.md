# refactor handoff

## What changed (files + one line each)
- internal/core/bead_claims.go: duplicated claim-verification loop deduped — `checkBeadClaims` and `checkBeadClaimsWithCache` now both route through one `evaluateClaims(beadID, claims, reproof)` helper returning (allow, allPass); `evaluateClaim` deleted (was a wrapper over the loop); behavior byte-identical (hard fail → refuse, unknown/soft fail → fail-open, allPass precondition for cache write preserved).
- internal/core/bead_claims_cache_test.go: test-name typo fixed — `TestClaimCache_ChangedBaseSHA_RepoofsAndRefreshesCache` → `...ReproofsAndRefreshesCache`.
- CHANGELOG.md: Unreleased/Changed entry.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit baseline): `go test ./internal/core/ -run 'BeadClaims' -count=1` — ok (0.630s). PASS.
- AFTER: same command — ok (0.619s, all cache tests incl. `ReproofsAndRefreshesCache` PASS). PASS.
- Gated run: `BEAD_CLAIMS_CACHE_ENFORCE=1 go test ./internal/core/ -run 'BeadClaims|ClaimCache' -count=1` — ok (0.643s). PASS.
- `go build ./internal/core/` — PASS. `go vet ./internal/core/` — clean. `gofmt -l internal/core/bead_claims.go` — clean.
- <5ms spawn-path bound: `go test ./internal/core/ -run 'TestClaimCache_WarmCacheSameBaseSHA_ReusesWithoutProbes' -count=1 -v` — PASS (0.00s cached path). PASS.

## Open edges for the next phase
- No benchmarks exist for the claim path; the 5ms assertion is wall-time in the test (still passing).
- No bugs found requiring `cm add`.
