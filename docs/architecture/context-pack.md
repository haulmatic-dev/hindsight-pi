# Context Pack

The context pack is the daemon-written orientation block that replaces the
first ~8 turns of worker discovery (measured 2026-07 across 967 sessions:
6,336 bash + 2,299 read calls vs 58 edits in the opening turns of a bead).
It is bead 1 of the orientation-burn reduction program; later beads add an
agentic scout fallback and a measurement harness.

## Contract

A pack is a JSON document stored in a bead's `agent_context` field
(`br update --agent-context`, beads_rust#297, br ≥ 0.2.10 — round-trip
verified). Schema (`ContextPackVersion = 1`):

```json
{
  "version": 1,
  "base_sha": "<working-branch HEAD at build time>",
  "gate_commands": ["go test ./...", "golangci-lint run"],
  "spec_pattern": "*_test.go (colocated next to source)",
  "target_dirs": ["cmd", "internal", "internal/core"],
  "files": [{"requirement": "CreateShortlink", "path": "shortlinks.go", "confidence": 1.0, "provenance": "mechanical"},
              {"requirement": "Flux capacitor calibration", "path": "flux/cap.go", "confidence": 0.85,
               "symbol": "Calibrate", "anchor_snippet": "func Calibrate() {", "provenance": "agentic"}],
  "member_digests": [{"id": "chain-1", "digest": "- chain-1: one-line digest for chain member 1"}],
  "footer": "elided: chain-2, chain-3",
  "provenance": "hydra-context-pack"
}
```

- `gate_commands` — from the project's `gates` block in hydra.yaml, canonical
  test/lint/build order (`gateCommandList`). These are the commands the
  daemon will run after the worker finishes; the prompt tells the worker to
  use them for scoped checks instead of guessing.
- `spec_pattern` — dominant test-file convention derived by counting glob
  families (`*_test.go`, `*.spec.ts`, `test_*.py`, …) over the tree, with a
  colocation hint when the majority sit next to a same-stem source file.
- `target_dirs` — repo directories two levels deep (hidden/vendored dirs
  skipped), so the worker's first `ls` is free.
- `files` — requirement→file anchors from two sources, told apart by the
  per-anchor `provenance` field:
  - `"mechanical"` (default) — the graphify symbol index
    (`graphify-out/graph.json`). Best-effort and fail-open: an absent or
    malformed graph yields an omitted section, never an error.
    `confidence < 1` means a substring/fuzzy match, not an exact symbol hit.
  - `"agentic"` — the scout pass (below). Carries the pinned `symbol` and a
    verbatim `anchor_snippet` line in addition to path/confidence.
- `provenance` — `"hydra-context-pack"` marks packs this daemon wrote; a
  human's hand-written `agent_context` never parses as a pack.
- `member_digests` — one one-line digest (`"- <id>: <title>"`) per chain
  member, so a spawned worker sees the chain's shape without re-fetching
  beads. Subject to the token budget (below).
- `footer` — after token-budget trimming, lists the elided bead IDs
  (`"elided: id1, id2, ..."`); empty when nothing was elided.

## Token budget

The pack is written to every chain member, so its size must stay bounded.
`ApplyPackTokenBudget` estimates the serialized pack's token size as
`chars / 4` and trims member digests oldest-first until the estimate fits
`context_pack.max_pack_tokens` (default `DefaultMaxPackTokens` = 40000;
`0` or unset in yaml = default). Three guarantees:

1. **Oldest-first elision** — digests are cut in original chain order
   (elided IDs appear in that order in the `footer`).
2. **The current bead's digest is never elided** — the worker spawning now
   always sees its own entry even when every other digest is cut.
3. **Exactly one typed event per elision pass** — `pack_trimmed` (reason
   `budget`, detail `elided_count=<n> budget=<tokens>`) is emitted once per
   `pack_created`/`pack_refreshed` write that had to trim; no event when the
   pack fit.

Config (`hydra.yaml`):

```yaml
context_pack:
  max_pack_tokens: 40000   # default; 0/unset = default
```

## Agentic scout fallback

The mechanical index maps what it can (exact symbol hit = 1.0, substring =
0.6). Requirements whose best anchor is below `PackScoutConfidenceThreshold`
(0.7) — or which have no anchor at all — are *unmapped*: greenfield areas,
novel concepts, repos without a graph. For those, the daemon runs ONE
bounded Pi scout pass per chain build (`context_pack.scout.enabled: true` —
**off by default**; enabling opts into scout spend):

- **Bounded** — `max_turns` (default 20) enforced two ways: the prompt tells
  the model the budget, and a wall-clock timeout of `max_turns × turn_budget`
  (default 60s/turn) kills the whole process group. The runner is
  `PiScoutRunner`: one headless `pi --offline` invocation with the fleet's
  provider/model identity, so the pass rides the normal llm-router lane
  accounting (cost attribution via the `pack_scout` event).
- **Read-only** — the prompt is an orientation brief: map each requirement to
  1–3 files with a calibrated confidence and a verbatim anchor snippet. No
  edits, no test runs. Output is parsed from the final JSON array
  (`parseScoutOutput`), tolerant of surrounding prose.
- **Merged, not replaced** — `MergeScoutMappings` appends scout mappings
  (provenance forced to `"agentic"`, deduped by requirement+path); the
  mechanical anchors stay.
- **Re-scout policy** — targeted, never whole-chain. At SHA refresh only
  agentic anchors that *broke* (`ValidatePackAnchors` dropped them) trigger a
  re-scout, and only for their own requirements
  (`DroppedAgenticRequirements`). Mechanical drops and never-scouted
  requirements never spawn a pass. A daemon-wide mutex (`scoutGate`)
  serializes passes so overlapping spawns cannot double-spend.
- **Fail-open** — scout error/timeout logs a `pack_scout` event with reason
  `scout_failed` and the spawn proceeds with the mechanical-only pack.

Config (`hydra.yaml`):

```yaml
context_pack:
  scout:
    enabled: true        # default false
    max_turns: 20        # turn cap
    turn_budget: 60s     # per-turn wall time; timeout = max_turns × turn_budget
    provider: ""         # default: pi.standard-agent.llm provider (fleet identity)
    model: ""            # default: pi.standard-agent.llm model
    api_key_name: ""     # default: KIMI_API_KEY
```

## Lifecycle

1. **Build** — at chain first-member spawn (`WorkerSpawner.ensureContextPack`,
   called from `SpawnInternal` after project-path resolution). Requirement
   terms come from all chain-member titles (`RequirementTermsForBead`);
   titles whose terms stay below the confidence threshold are handed to the
   scout pass (see “Agentic scout fallback”).
   Non-chain (singleton) beads are treated as a chain of one
   (`AllBeadIDs=[self]`), so standalone fix-beads get the same pack and
   SHA-keyed refresh instead of spawning unguided.
2. **Storage** — serialized and written to **every** chain member via
   `BeadsClient.UpdateBeadAgentContext` (`br update <id> --agent-context
   @file`; the `@file` form avoids shell-quoting breakage). Per-member write
   failures log and continue — fail-open: a partially guided chain beats an
   unspawned one.
3. **Refresh** — at each later member spawn the pack's `base_sha` is compared
   against the working-branch HEAD (`currentGitHead`). Equal → reuse.
   Moved → the mechanical sections are rebuilt from the current tree and the
   `files` anchors are **re-validated, not regenerated**:
   `ValidatePackAnchors` drops anchors whose file no longer exists, emitting
   one `pack_miss` event per drop. Dropped *agentic* anchors then trigger a
   targeted per-requirement re-scout (never a whole-chain redo).
4. **Injection** — `buildPiPrompt` (cmd/worker) parses the bead's
   `agent_context` and appends the rendered pack to the **per-bead variable
   tail only**. The invariant doctrine head is byte-identical with or without
   a pack (pinned by `TestBuildPiPrompt_PackKeepsStableHead`); this keeps the
   95%-cache-hit prefix-stability program intact. The tail also carries three
   behaviour lines: no approval-seeking (the bead is the approval), parallel
   tool calls, and the identical-error-twice failure protocol.
5. **Enforcement** — verified close's scope gate
   (`BeadCloser.checkTargetScope`) consults the pack when the bead declares
   no Target Files: pack `files` anchors become the enforcement target set,
   so a guided bead that edits outside its anchors is rejected in `block`
   mode exactly like a target-files bead. Because the pack is written to
   every chain member, the bead itself is the chain lookup — no extra
   resolution at close time. File reservations are also consulted:
   `Overseer.consultPackOnReservation` checks each worker-initiated
   `file_reserved` message against the pack anchors and emits an advisory
   `pack_reservation_offscope` event for off-anchor reservations. Hydra's
   reservation stream is informational (the daemon records it; worker-to-
   worker conflict enforcement lives in the MCP reservation client), so the
   reservation consult is advisory by design — the blocking enforcement is
   the close-time scope gate above.

## Events

Typed events on the fail-open event stream (never gate the pipeline):
`pack_created`, `pack_refreshed` (per member write, reason carries the short
SHA), `pack_miss` (one per dropped anchor, reason `anchor_gone`),
`pack_trimmed` (one per `pack_created`/`pack_refreshed` write whose member
digests were token-budget-trimmed; reason `budget`, detail carries
`elided_count` and `budget`),
`pack_scout` (one per scout pass — reason `scout_ok`/`scout_failed`, detail
carries requirement/mapping counts and duration for cost attribution),
`pack_reservation_offscope` (advisory, reason `off_anchor_reservation`),
`pack_breaker_open` (one per open — a project's pack writes failed 3 times
consecutively; detail carries the failure count and backoff) and
`pack_breaker_close` (writes succeeding again, breaker reset).

## Write circuit breaker

When a project's br store is unwritable by the daemon user, pack writes would
otherwise be retried on every 2-3s scheduler tick, generating exactly the br
lock contention that blinds the DAG builder. `ensureContextPack` therefore
rides a per-project circuit breaker (state in `WorkerSpawner.packBreakers`,
logic in `context_pack.go`): each failed write pass schedules exponential
backoff (1m doubling per consecutive failure, 30m cap) during which spawns
proceed unguided; at 3 consecutive failures the breaker opens and emits one
`pack_breaker_open` event (repeats stay quiet until reset); a fully
successful write pass emits `pack_breaker_close` and resets the breaker.
Breaker accounting is per write pass, not per chain member, so large chains
cannot trip it on a single tick. Skip and open paths stay fail-open: the
pack is an optimization and never gates a spawn.

## Failure model

Fail-open everywhere. Build errors omit sections; graph absence omits the
files section; write errors skip that member; a scout error/timeout keeps
the mechanical-only pack; a panic in
`ensureContextPack` is recovered and logged. The pack is an optimization and
must never block a spawn — today's unguided prompt is the floor.
