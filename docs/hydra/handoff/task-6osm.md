# discovery handoff (hydra-6osm)

## What changed (files + one line each)
- docs/discovery/claim-respawn-cache-discovery-6osm.md: discovery doc for FR-002 claim re-proof at respawn — fingerprint design, durable cache labels (`claims-ok-<first8>` + `claims-ok-at-<unix>`), base_sha (worktree HEAD) reuse/re-proof split in `spawnPreflightBeadClaims`, `claim_dropped {bead, assertion}` contract (mirror of `pack_miss`, fail-closed `hard:` claims never weakened by dropping), <5ms cached-path note + test shape, AC mapping.
- CHANGELOG.md: Documentation entry under Unreleased.
- No code change (discovery bead).

## Legacy notes
- Known fallback: older worktrees used `osLookupEnv` for cache toggles and the deprecated `*exec.Error`-only tooling classifier; superseded by `classifyClaimStatus` (landing already on this branch).

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'BeadClaims' -count=1` — PASS (`ok github.com/haulmatic-dev/hydra/internal/core`). AC4 met at discovery time.
- Gate premise check: /opt/dev-tools/var/hydra/gates/hydra/hydra-6osm/build.log absent (stale "build FAILED exit -1"); the drift-check implementation and its suite are present and green on this worktree.
- `hindsight_search` at bead start — not connected (skip rule applied). graphify-out/graph.json absent in cwd → rg + handoff/chain-context fallback per protocol.

## Open edges for the next phase
- GREEN: implement `claimFingerprint`, cache read/write in `spawnPreflightBeadClaims` (labels via `o.beadsClient`), base_sha resolution via `repoDirForBead` (single projects = project path), `claim_dropped` emit, and the <5ms wall-time test with an uninvoked runner assertion.
- Multi-repo base_sha default is project path when repo resolution fails (fail-open).
