# green handoff (ucdo — claim re-proof cache)

## What changed (files + one line each)
- internal/core/bead_claims.go: `claimCacheImplemented=true`; `claimCacheFingerprint` (sha256 of bead id + payload + worktree HEAD, 8-hex prefix), `hasLabel`/`hasLabelPrefix` helpers, real production `claimHeadProbe` (`git -C <projectPath> rev-parse HEAD`; empty/failed probe → "" → every spawn re-proofs); `verifyClaim` extracted (returns status; emits `claim_dropped` on a reproof soft-fail), `checkBeadClaimsWithCache` — cached `claims-ok-<fp8>`+`claims-ok-at-*` labels reuse the pass without probes; cache miss re-proofs and rewrites labels only when every claim passed.
- internal/core/spawn_preflight.go: `spawnPreflightBeadClaims` now runs the cache-aware check (`claimCheckRunner` seam threaded so tests/prod both hit the runner seam) and persists cache labels via `beadsClient.UpdateBeadLabels(projectPath, "", bead.ID, ...)`.
- internal/core/stranded_work.go: local duplicate `hasLabel` removed (single copy in bead_claims.go).
- internal/core/bead_claims_cache_test.go: helper-only additions — `seedClaimBead(o, desc)` registers the fixture bead with the mock store (mirrors real br where the bead always exists); no assertions touched.
- CHANGELOG.md: GREEN entry under Unreleased/Added.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS. `go vet ./internal/core/` — clean. `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run 'BeadClaims|TestClaimCache' -count=1` — PASS, 0 failures (7 cache contract tests + full drift-check suite). VERIFIED.
- `go test ./internal/core/ -run 'TestDetectStrandedWork|TestIntegrateStrandedRepair|TestClaimCache|BeadClaims|TestStoreOwnership|TestWorkerCompletion|TestVerifyBeforeClose' -count=1` — PASS (regression set over callers of the shared helper).
- Coverage (`-coverprofile` over BeadClaims|TestClaimCache): `checkBeadClaimsWithCache` 94.4%, `claimCacheFingerprint` 100%, `hasLabel` 100%, `hasLabelPrefix` 75%, `verifyClaim` 87.5%, `spawnPreflightBeadClaims` 90.9%.

## Open edges for the next phase
- Production `claimHeadProbe` exec body is untested (seam covered); it is a single git call, fail-open by design.
- Cache labels append only (br semantics); a payload change leaves stale `claims-ok-<oldfp>` labels — harmless (fingerprint no longer matches).
NOT VERIFIED by this bead: none within scope.
