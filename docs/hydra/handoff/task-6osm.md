# discovery handoff (task-6osm)

## What changed (files + one line each)
- docs/discovery/claim-reproof-respawn-6osm.md: FR-002 discovery — verified the feature is absent on this branch (0 grep hits for claims-ok/claim_dropped/re-proof in Go code), mapped each AC to its reuse site (ContextPack.BaseSHA compare at worker_spawner.go:918, pack_miss event at context_pack.go:525, prespawn probe at prespawn_probe.go:39, VerificationRunner.RunWithEvents), and pinned the GREEN contract: `bead_claims.go` with `claimFingerprint` (sha256 bead_id+probeKey+baseSHA, first 8 hex), `readCachedClaims`, `recordClaimPass`, `revalidateClaims` emitting one `claim_dropped {bead, assertion}` per broken claim; payload = Labels `claims-ok-<first8>` + metadata `claims_ok_ts`; fail-open throughout; spawn-path wire-in before probe execution.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verification (commands run + pass/fail)
- `hindsight_search` at bead start — not connected (skip rule applied).
- `graphify query` — no claim-related community found; `rg` fallback used.
- `rg 'claims-ok|VerificationClaim|claim_dropped|re-proof' --type go` — 0 hits (feature absent, VERIFIED).
- `go test ./internal/core/ -run 'BeadClaims' -count=1` — ok, "no tests to run", exit 0 (AC4 pass pre-RED; RED phase adds the suite).
- `git show --stat HEAD` — commit 6c77d854: 2 files, +41 (doc + changelog).

## Open edges for the next phase
- RED must add `internal/core/bead_claims_test.go` with `TestBeadClaims*` names (AC4's `-run 'BeadClaims'` selector) pinning: cache reuse without re-probe, base_sha move → re-proof + one `claim_dropped` per dropped assertion, <5ms cached-path wall-time assert, empty-git-head treated as cache-miss (not "changed").
- Label REPLACE semantics (one `claims-ok-<first8>` per probe key, not append) — mirrors verified-close-reject-attempt-N; named in the discovery doc's risks section.
- Config: no knob planned (fail-open optimization); only a single label-prefix constant, mirroring `panFiledLabel`.
