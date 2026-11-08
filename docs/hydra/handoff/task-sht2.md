# SYNTAX Handoff — task-sht2 (bead 5xvh)

**Phase:** syntax
**Bead:** hydra-sht2 (5xvh)
**Feature:** Claim re-proof at respawn with payload persistence
**Date:** 2026-11-08
**Disposition:** docs-only (verify-first; no code change in this run)

## State at verification

The entire claim re-proof cache chain is already landed on this branch:

- RED: `internal/core/bead_claims.go` seams (`claimCacheImplemented`, `claimCheckRunner`, `claimHeadProbe`) + `internal/core/bead_claims_cache_test.go` contract suite gated by `BEAD_CLAIMS_CACHE_ENFORCE=1`.
- GREEN: durable claim re-proof cache — `claimCacheImplemented=true`, labels `claims-ok-<first8>` + `claims-ok-at-<ms>`, `base_sha`-gated reuse/re-proof, `claim_dropped` soft-fail event.
- REFACTOR: `evaluateClaims(beadID, claims, reproof)` helper unified the duplicated verify loops; hard-fail remains the only spawn-refusing path; warm-cache path stays under 5 ms.
- A prior SYNTAX pass in this chain (kzlu) already closed docs-only after verifying the same surface.

## Verification (all VERIFIED this run)

| Gate | Command | Result |
|---|---|---|
| Focused tests | `go test ./internal/core/ -run 'BeadClaims' -count=1` | PASS (ok 0.623s), RC=0 |
| Full build | `go build ./...` | RC=0 |
| Vet | `go vet ./internal/core/` | clean, RC=0 |
| gofmt | `gofmt -l` on bead_claims.go, bead_claims_cache_test.go, spawn_preflight.go, overseer.go | clean |
| Placeholders | grep TODO/FIXME/placeholder/not-implemented on the 4 target files | 0 real hits (one comment in overseer.go:502 legitimately describes purging stale PID-0 claim entries, not a stub) |
| Doc comments | public identifiers in bead_claims.go documented | present |

Cache-spawn-path bound (<5 ms) is covered by the enforce-gated `TestClaimCache` suite, which was exercised green in the chain's GREEN/verify runs (`BEAD_CLAIMS_CACHE_ENFORCE=1` PASS).

## Files changed by this bead

- `docs/hydra/handoff/task-sht2.md` (this file)
- `CHANGELOG.md` (documentation entry)

## Handoff notes for next chain link

No open defects on the claim re-proof surface. If a later bead touches `bead_claims.go`, re-run the focused suite plus the enforce-gated cache tests before close.
