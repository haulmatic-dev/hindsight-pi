# syntax handoff (kzlu — Claim re-proof at respawn with payload persistence)

## What changed (files + one line each)
- None in source (verification-only SYNTAX pass). Claim-cache GREEN (claimCacheImplemented=true, `claims-ok-<first8>` + `claims-ok-at-<ms>` labels, base_sha-gated reuse/re-proof) and the `evaluateClaims` refactor are committed on this branch (commits 1e37712b refactor, 06a5e6bf feat). All target files conform.
- docs/hydra/handoff/syntax-kzlu.md: this handoff.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'BeadClaims' -count=1` — PASS (`ok github.com/haulmatic-dev/hydra/internal/core 0.623s`). AC4 MET.
- `go test ./internal/core/ -run 'TestClaimCache_' -count=1 -v` — PASS 7/7 (PassOnFirstSpawnWritesLabels, WarmCacheSameBaseSHA_ReusesWithoutProbes, ChangedBaseSHA_ReproofsAndRefreshesCache, Reproof_BrokenClaimDropped, Reproof_BrokenHardClaimRefusesSpawn, UnknownNeverCached, FailedClaimsNeverCached).
- `BEAD_CLAIMS_CACHE_ENFORCE=1 go test ./internal/core/ -run 'BeadClaims|ClaimCache' -count=1` — PASS (ok 0.634s). Gate green.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on bead_claims.go / bead_claims_cache_test.go / spawn_preflight.go / overseer.go — clean.
- Placeholder grep (`// TODO|// FIXME|// Placeholder`) over the three target files — 0 hits; bead_claims.go 339 lines, test 340, non-empty.
- Doc comments verified on the claim-cache surface: `claimVerifyTimeout`, `claimEventRecorder`, `claimCacheImplemented`, `claimCheckRunner`, `claimHeadProbe`, cache label convention comment block (bead_claims.go:19–78).
- `go build ./...` — FAIL, but ONLY in `cmd/worker` from a concurrent strand's UNCOMMITTED `cmd/worker/enrichment.go` (+338/−37, `enrichment.go:166:4: undefined: log`, `280:2: declared and not used: removed`). Not this chain's file (chain scope = internal/core); `internal/core` itself compiles and its suite is green. Left untouched per smallest-change discipline — the enrichment strand will fix its own file.

## Open edges for the next phase
- Concurrent enrichment.go dirty state must be resolved by its owning strand before any gate that runs `go build ./...` passes.
- Cached path <5ms wall bound asserted by TestClaimCache_WarmCacheSameBaseSHA_ReusesWithoutProbes (passes).
