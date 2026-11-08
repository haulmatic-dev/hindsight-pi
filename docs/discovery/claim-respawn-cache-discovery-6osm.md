# Discovery — FR-002 Claim re-proof at respawn with payload persistence (hydra-6osm)

Scope: cache verified-claim results across respawns so a respawned bead does not
re-run its claim probes when the evidence environment has not moved, and
re-proofs fully (with claim drops) when it has.

## Current state (verified on HEAD)

- `internal/core/bead_claims.go`: claims parsed from the bead description
  (`[hard: ]claim: <assertion> (verify: <cmd>)`), probe verdict triple
  pass/fail/unknown, `hard:` fail refuses spawn (`claim_hard_failed`), soft
  fail warns (`claim_soft_failed`), tooling failure fails open
  (`claim_unknown`). Zero claims → no-op.
- `internal/core/spawn_preflight.go:256` `spawnPreflightBeadClaims` — runs
  `beadClaimChecker.checkBeadClaims` on EVERY spawn attempt, including every
  respawn/retry. There is no memory between attempts: every retry of a bead
  re-runs its verify commands (up to 2s each).
- `internal/core/context_pack.go` / `worker_spawner.go` — ContextPack already
  carries `BaseSHA` (worktree HEAD) and already implements the exact lifecycle
  this FR wants mirrored for claims: fresh/refresh (`pack_refreshed`) /
  dropped anchors with per-drop `pack_miss` events.
- Gate claim in the bead body ("daemon gate build FAILED exit -1") is stale for
  this file set: `go test ./internal/core/ -run 'BeadClaims|DriftCheck'
  -count=1` → `ok` on this worktree HEAD.

## Design

### 1. Fingerprint and cache shape (ambiguity resolved)

- `claimFingerprint(payload)` = first 8 hex of
  `sha256(payload || "\x00" + each claim rendered canonically
  "hard:<bool>|<assertion>|<verify>" joined "\n")`. Payload-inclusive, so any
  bead description edit changes the fingerprint (no stale reuse after a claim
  is edited in the payload).
- Cache lives on the bead itself (payload persistence, survives worker and
  daemon restarts):
  - label `claims-ok-<first8>` — durable pass marker.
  - label `claims-ok-at-<unix>` — cache timestamp (labels cannot carry a
    value; a companion label keeps the store schema untouched).
  - GREEN may consolidate into metadata `claims_ok` `{fingerprint, at,
    base_sha}` if the two-label shape proves noisy; AC text names the label +
    a timestamp, and metadata satisfies "on the bead payload" equally.
- Cached passes apply ONLY to claims classified `pass` at cache time. `unknown`
  results are never cached (tooling failure may recover); `fail` results are
  never cached (evidence, not freshness).

### 2. Respawn flow in `spawnPreflightBeadClaims`

1. Parse claims; zero claims → return immediately (no-op, as today).
2. Compute fingerprint. If bead labels contain `claims-ok-<fp8>`:
   - Resolve the worktree HEAD (`git -C projectPath rev-parse HEAD`, one exec
     ~1–2ms; budget note below). If head matches the recorded `base_sha`
     (label `claims-ok-at` and head cached alongside — GREEN stores
     `base_sha` in the companion metadata/label) → **reuse**: no probe runs,
     spawn proceeds. This is the <5ms path.
   - If head differs → full re-proof as below, and every claim that now PASSES
     refreshes the cache (new fingerprint/labels).
   - If a previously-cached claim now FAILS (re-proof path only): the claim is
     dropped from the injected payload set and a typed `claim_dropped` event
     `{bead, assertion}` is emitted (mirror: one event per dropped claim, same
     shape as `pack_miss`). Behavior per claim class: soft dropped claim =
     warn + proceed; `hard:` dropped claim still refuses the spawn
     (`claim_hard_failed`) — dropping removes it from the worker's injected
     payload so the agent does not act on dead assertions, it does NOT weaken
     the fail-closed gate.
3. No cache label → probe everything (current behavior), then cache on full
   pass (all claims pass → write labels + timestamp + base_sha).

### 3. Where "the injected payload" is

Claims are parsed from `bead.Description` (the payload injected into the
agent context, plus the agent_context pack). Dropping a claim means the
spawn-time payload handed to the worker no longer carries the dead claim line
(same mechanical-section-edit model ContextPack already uses for anchors).

## AC mapping

| AC | Resolution |
|---|---|
| cache `claims-ok-<first8>` + timestamp; respawn reuses without re-running probes unless base_sha changed | §1–§2; GREEN implements read-cached/probe-missed split in `spawnPreflightBeadClaims` |
| changed base_sha → full re-proof; broken claims dropped with `claim_dropped {bead, assertion}` (mirror of `pack_miss`) | §2; event via existing `EventSink` (nil-safe, fail-open) |
| cached-pass reuse <5ms (test asserts wall-time bound) | cached path = 2 label reads + 1 `rev-parse`; test asserts end-to-end `spawnPreflightBeadClaims` wall time <5ms with cache warm (probe runner asserted uninvoked) |
| `go test ./internal/core/ -run 'BeadClaims' -count=1` exits 0 | verified PASS today; GREEN must keep it green |

## Open edges for GREEN

- base_sha for single projects = project checkout head; multi-repoprojects
  should use the repo dir that owns the bead (`repoDirForBead`) — default to
  project path when repo resolution fails (fail-open).
- Clock for `claims-ok-at`: `time.Now` daemon-side; no TTL is specified by the
  AC (base_sha change is the invalidation), so timestamp is observational.
-Race: a concurrent label write from the close path is benign (labels are
  additive; last write wins with a fresh fingerprint).
