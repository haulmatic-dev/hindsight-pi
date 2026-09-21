# SoL-Pi pilot: Action Fusion + ObservationPack

**Status:** parked → ready for pilot (worker support landed 2026-09-22)
**Scope:** pilot ONLY the vendor's conservative config. Evidence-Preserving
Reducer (extra remote model call) and Online Context Compact (starts a new
turn after compacting — conflicts with the frozen-prefix prompt-cache
program) stay disabled, in config and by policy.
**Host pin:** `@earendil-works/pi-coding-agent` 0.85.1 (fleet-verified
2026-09-20; SoL-Pi peer range `0.84.2 || 0.85.1`). Node ≥ 22.19 (fleet has
v22.22.2).

## What SoL-Pi is

`npm:sol-pi@0.1.0` (MIT, NVIDIA) — a Pi extension packaging four efficiency
mechanisms. The two piloted here:

- **Action Fusion** — an edit/write tool call can carry `then_run`, running
  the follow-up validation command in the same tool call. Targets the
  edit→test ping-pong that dominates GREEN/REFACTOR p90 turn counts.
- **ObservationPack** — repeated large tool results become stable handles
  with exact paged recall (`obs_recall`). Archives live under
  `<session-dir>/sol-pi/<session-id>/` — workers use persistent sessions
  (`--session-id` + `--session-dir`), so archival works.

Both make no extra model calls and never stop an active run.

## How the worker integrates it

No daemon changes. The existing per-phase allowlist chain does everything:

```yaml
agents:
  pi:
    extensions: [rtk, context-mode]              # fleet default unchanged
    phase_extensions:
      green: [rtk, context-mode, sol-pi]         # pilot phases only
      refactor: [rtk, context-mode, sol-pi]
```

1. The daemon resolves `ExtensionsForPhase` → `HYDRA_PI_EXTENSIONS` env
   (`internal/core/worker_env_builder.go`).
2. The worker resolves `sol-pi` to its declared entry
   (`./src/sol-pi/index.ts`) under `{config_dir}/npm/node_modules` — the same
   resolver rtk uses; TS entries and host-provided peers are proven in the
   fleet (rtk ships an unbuilt `./index.ts` with unmet peers and works).
3. `solPiResolved` gates provisioning: config is written and trust granted
   only when `sol-pi` is allowlisted **and** its package actually resolves.
4. `provisionSolPi` writes `<worktree>/.pi/sol-pi.json` (conservative
   profile below) and appends `.pi/` to the worktree-local
   `.git/info/exclude` so target repos without a `.pi` gitignore never
   commit it. An existing project `sol-pi.json` is kept — project config
   wins.
5. The worker passes `--approve` for that run only. SoL-Pi reads its config
   from `<cwd>/.pi/sol-pi.json` only when `ctx.isProjectTrusted()`; rpc mode
   never prompts, so without this the extension would load with everything
   disabled. `--approve` also enables project-local settings/packages for
   the run — pilot beads should initially be hydra-repo dogfood (its `.pi/`
   is gitignored, skills-only) or repos with no `.pi/` tree.
6. If provisioning fails (unwritable worktree), the worker logs and skips
   `--approve`: sol-pi loads with defaults = all mechanisms disabled =
   silent no-op. Fail-open by design — a mistyped pilot must never cost a
   bead.

Effective config written per worktree:

```json
{
  "version": 1,
  "actionFusion": true,
  "observationPack": true,
  "evidencePreservingReducer": false,
  "evidencePreservingReducerRemote": false,
  "onlineContextCompact": false,
  "cacheWriteReadRatio": 12.5
}
```

## Operator steps to start the pilot

1. Install into the shared config (one machine, fleet-wide effect):

   ```bash
   cd /opt/dev-tools/config/pi/npm
   npm install sol-pi@0.1.0 --legacy-peer-deps
   ```

   `--legacy-peer-deps` is deliberate: the Pi host provides
   `@earendil-works/*` at runtime (rtk precedent), and auto-installing
   peers would duplicate the whole Pi tree into the shared config.

2. Verify the resolver sees it (run as the hydra user, matching worker env):

   ```bash
   PI_CODING_AGENT_DIR=$(scripts/... resolvePiAgentDir equivalent)
   pi list --approve | grep -i sol-pi
   ```

3. Add the `phase_extensions` block to `/etc/hydra/hydra.yaml` (snippet
   above). Restart the daemon.
4. Confirm in a worker log: `[Worker:rpc] loading N allowlisted extension(s):
   ..., sol-pi` followed by `sol-pi pilot: wrote .pi/sol-pi.json, granting
   per-run project trust`.

## A/B protocol (turn-economy harness)

`scripts/turn_economy.py` (`docs/operations/turn-economy-measurement.md`) is
the measurement instrument. Matched-bead design:

1. **Baseline campaign** — run matched bead types (same repo, same phase,
   comparable target-files scope) WITHOUT sol-pi; collect sessions into a
   `pre` campaign window.
2. **Pilot campaign** — enable `sol-pi` on `green`/`refactor` (the phases
   with measured edit→test ping-pong), run the matched set, collect into a
   `post` campaign window.
3. **Compare** — `--compare-before pre --compare-after post`. Primary
   metrics: turns p50/p75, tokens (input/output/cacheWrite), cmd-rerun
   rate, per-phase turn distribution. Expect Action Fusion to show as fewer
   edit→bash validation cycles (cmd-rerun down or shifted into fused
   `then_run` receipts — check the transcripts for `then_run` in tool
   calls) and ObservationPack as lower input-token replay.
4. **Guardrails (blocking)** — gate first-try pass rate must not fall;
   empty-patch rate must not rise. A guardrail regression exits 2 and
   voids the pilot claim regardless of turn savings.
5. **Decision record** — append results here: campaigns, denominators,
   deltas, guardrail verdict, and the keep/rollback call.

## Rollback

1. Remove `sol-pi` from `phase_extensions` in `/etc/hydra/hydra.yaml`;
   restart the daemon. Workers immediately stop loading it (the next spawn
   has no `-e sol-pi` and no `--approve`).
2. Optionally `npm uninstall sol-pi` in the shared config.
3. Per-worktree `.pi/sol-pi.json` files are gitignored/excluded and die with
   the worktree; no cleanup needed.

No worker code rollback is needed for the allowlist removal alone — with
sol-pi not resolved, neither provisioning nor `--approve` happens.
