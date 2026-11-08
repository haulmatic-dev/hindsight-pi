# red handoff

## What changed (files + one line each)
- internal/core/bead_claims_cache_test.go (new): 6 RED contract tests for "Claim re-proof at respawn with payload persistence" — full pass on first spawn writes durable labels `claims-ok-<first8>` (8-hex fingerprint) + `claims-ok-at-<unix>` via UpdateBeadLabels; warm-cache + unchanged base_sha respawn reuses without re-running probes (wall-time <5ms asserted, probe count asserted 0); changed base_sha → full re-proof (probes re-run, claims-ok-at refreshed); re-proved broken soft claim → exactly one typed `claim_dropped` event {bead, assertion in Detail}, spawn proceeds; re-proved broken `hard:` claim still refuses spawn (SpawnSuppressedError — fail-closed gate not weakened by dropping); unknown (DeadlineExceeded) outcomes never cached; failing claims with no prior cache never cached. Gate: `claimCacheImplementedGate` skips loudly by default, `BEAD_CLAIMS_CACHE_ENFORCE=1` fails them.
- internal/core/bead_claims.go: inert RED seams only — `claimCacheImplemented=false` const, `claimCheckRunner beadClaimRunner` var (nil = current behavior), `claimHeadProbe` var returning ("", nil) (inert until GREEN). Zero behavior change.

## Contract GREEN must implement
- Cache write on all-claims-pass in `spawnPreflightBeadClaims`: labels `claims-ok-<fp8>` + `claims-ok-at-<unix>` (+ base_sha recorded, per discovery doc §1; metadata consolidation allowed).
- Respawn read: labels present + HEAD via `claimHeadProbe(projectPath)` matches recorded base_sha → reuse, no probes, <5ms.
- Head differs → re-proof: passing claims refresh cache; failing soft claim emits `claim_dropped {bead, assertion}` (one per claim, mirror of `pack_miss`) and spawn proceeds; failing hard claim emits `claim_dropped` AND refuses spawn (`SpawnSuppressedError`, `claim_hard_failed` semantics preserved).
- Only `pass` outcomes cached; `unknown` and `fail` never cached.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS ( seams compile; package green).
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l internal/core/bead_claims_cache_test.go internal/core/bead_claims.go` — clean.
- `go test ./internal/core/ -run 'BeadClaims|ClaimCache' -count=1` — PASS, exit 0 (6 new tests skip loudly: "RED: claim re-proof cache contract not yet implemented"; drift-check suite green).
- `BEAD_CLAIMS_CACHE_ENFORCE=1 go test ./internal/core/ -run 'ClaimCache' -count=1` — FAIL: all 6 tests fail with `RED: claim re-proof cache contract not yet implemented` — RED confirmed for the RIGHT reason (missing feature, not compile error).

## Open edges for the next phase
- Multi-repo projects: base_sha should come from `repoDirForBead` (discovery doc edge); tests use projectPath head seam.
- No TTL on cache per AC; timestamp is observational.
- GREEN must keep `go test ./internal/core/ -run 'BeadClaims' -count=1` exit 0.
