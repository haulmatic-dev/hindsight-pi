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
  "files": [{"requirement": "CreateShortlink", "path": "shortlinks.go", "confidence": 1.0}],
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
- `files` — requirement→file anchors from the graphify symbol index
  (`graphify-out/graph.json`). Best-effort and fail-open: an absent or
  malformed graph yields an omitted section, never an error. `confidence < 1`
  means a substring/fuzzy match, not an exact symbol hit.
- `provenance` — `"hydra-context-pack"` marks packs this daemon wrote; a
  human's hand-written `agent_context` never parses as a pack.

## Lifecycle

1. **Build** — at chain first-member spawn (`WorkerSpawner.ensureContextPack`,
   called from `SpawnInternal` after project-path resolution). Requirement
   terms come from all chain-member titles (`RequirementTermsForBead`).
   Non-chain (singleton) beads get no pack and spawn unguided.
2. **Storage** — serialized and written to **every** chain member via
   `BeadsClient.UpdateBeadAgentContext` (`br update <id> --agent-context
   @file`; the `@file` form avoids shell-quoting breakage). Per-member write
   failures log and continue — fail-open: a partially guided chain beats an
   unspawned one.
3. **Refresh** — at each later member spawn the pack's `base_sha` is compared
   against the working-branch HEAD (`currentGitHead`). Equal → reuse.
   Moved → the mechanical sections are rebuilt from the current tree and the
   agentic `files` anchors are **re-validated, not regenerated**:
   `ValidatePackAnchors` drops anchors whose file no longer exists, emitting
   one `pack_miss` event per drop.
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
`pack_reservation_offscope` (advisory, reason `off_anchor_reservation`).

## Failure model

Fail-open everywhere. Build errors omit sections; graph absence omits the
files section; write errors skip that member; a panic in
`ensureContextPack` is recovered and logged. The pack is an optimization and
must never block a spawn — today's unguided prompt is the floor.
