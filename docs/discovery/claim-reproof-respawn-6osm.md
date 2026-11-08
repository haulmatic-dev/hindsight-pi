# Discovery: Claim re-proof at respawn with payload persistence (FR-002 / hydra-6osm)

Verified on worktree HEAD `1ca06d3b` (`hindsight_search` unavailable → skip rule applied; `graphify query` returned no claim-related community — graph is discovery-doc heavy, fell back to `rg`).

## What the ACs map to in today's code

The feature does NOT exist anywhere on this branch (`rg 'claims-ok|VerificationClaim|claim_dropped|re-proof'` → 0 hits in Go code and docs). Every piece has an existing neighbor to build on:

| AC | Existing mechanism to reuse | Location |
|---|---|---|
| Cache verified-claim results on bead payload | Bead `Metadata map[string]string` + `Labels []string` (labels for `claims-ok-<first8>`, metadata for timestamp) | `pkg/types/bead.go:164-184` |
| "worktree base_sha changed" invalidation | `ContextPack.BaseSHA` + `currentGitHead(projectPath)` compare in `ensureContextPack` | `internal/core/context_pack.go:81`, `internal/core/worker_spawner.go:918-1035` |
| `claim_dropped` mirror of `pack_miss` | `logPackMiss(sink, project, bead, dropped)` typed-event helper, one event per dropped item | `internal/core/context_pack.go:525-534` |
| Run the probe | `VerificationRunner.RunWithEvents` (argv-only, cred-scrubbed, provenance) or scoped `GateRunner.RunGate` — same class as the prespawn probe | `internal/core/verification_runner.go:232`, `internal/core/prespawn_probe.go:39` |
| Spawn-path reuse | `ensureContextPack` already does the full SHA-compare/reuse/drop lifecycle at spawn; claim re-proof is the same pattern one layer up | `internal/core/worker_spawner.go:918-1035` |
| `go test ./internal/core/ -run 'BeadClaims' -count=1` | no `BeadClaims*` tests exist yet; command currently exits 0 ("no tests to run") — RED phase must add them | — |

## Resolved ambiguities (binding for GREEN)

1. **"Bead payload" = bead `Metadata` + `Labels`, not AgentContext.** Labels carry `claims-ok-<first8>` (visible to label-scan paths like pan/digest); a `metadata["claims_ok_ts"]` key carries the wall-clock timestamp (labels cannot). AgentContext is owned by the context pack — do not co-locate.
2. **"first8" = first 8 hex chars of `sha256(bead_id + probe key + base_sha)`** — same derivation family as `getVerificationFingerprint` (FR-006) but keyed on the PROBE, not the close-reject reason. This makes the cached label self-invalidating when base_sha moves (different hash ⇒ old label never matches ⇒ stale cache can never be consumed).
3. **Scope of "claims" = the daemon-side pre-spawn/close-path probes** (scoped gate probe, verification-runner acceptance). NOT arbitrary worker verdicts — worker verdicts are already handled by verified-close/fingerprint machinery.
4. **Respawn = any `spawnWorker`/`prespawnCloseIfGreen` path**, mirroring where `ensureContextPack` already sits. Base_sha changed ⇒ full re-proof (re-run every probe); unchanged + cached pass ⇒ skip probes, <5ms.
5. **Fail-open throughout** (same doctrine as pack and prespawn probe): cache read/parse errors, label-write errors, timestamp parse errors never block a spawn; they just fall through to a fresh probe run.
6. **`claim_dropped` event**: `Event{Type:"claim_dropped", Project, Bead, Fields:{"assertion": <probe key>}}`, emitted once per broken claim when base_sha moved — structurally identical to `logPackMiss`. Nil-sink safe, never gates.

## Remaining gaps GREEN must close (contract)

- `internal/core/bead_claims.go`: `claimFingerprint(beadID, probeKey, baseSHA)`, `readCachedClaims(bead)`, `recordClaimPass(beadsClient, bead, projectPath, probeKey, baseSHA)` (UpdateBeadLabels + metadata ts, fail-open), `revalidateClaims(...) -> kept, dropped` (compare stored base_sha vs `currentGitHead`; emit `claim_dropped` per drop via EventSink).
- Wire `revalidateClaims` into the prespawn/spawn path before probe execution.
- RED tests named `TestBeadClaims*` in `internal/core/bead_claims_test.go` pinning: cache reuse (no probe re-run), base_sha move → re-proof + `claim_dropped` per dropped assertion, cached-pass path adds <5ms wall-time (assert elapsed), and the AC4 suite command exits 0.
- Config: no knob required (optimization, fail-open), but a `claims` label prefix constant belongs in one place (mirror `panFiledLabel`).

## Risks / open edges

- Label churn: a flaky probe on a respawning bead could accumulate `claims-ok-*` labels; GREEN should REPLACE the previous label per probe key, not append (mirrors `verified-close-reject-attempt-N` replace semantics).
- `currentGitHead` returns "" on non-git dirs (prespawn probe uses `AllowMainCheckout`); empty head must NOT be treated as "base_sha changed" (treat unknown as cache-miss → re-proof once, then re-cache).
