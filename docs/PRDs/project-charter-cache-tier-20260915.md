# Project charter tier: shared per-project prompt prefix (idea PRD)

Status: idea — analysis-backed, recommended for build after Tier-1 Pan fixes
Date: 2026-09-15
Depends on: stable invariant head (#228), chain handoff artifacts (#229),
prefix-SHA logging (#228), llm-router affinity + cheapest-provider routing

## Problem

Cross-bead cache sharing today stops at the fleet tier: pi system + skills +
the 1.7KB doctrine head are shared; everything project-specific is
re-discovered per bead via billed tool calls. The 2026-09-15 discovery wave
is the measured proof: 5 parallel discovery beads in srv-smart-engine each
independently re-derived the same repo schema, conventions, and key-file
map — 5× the discovery cost, zero shared prefix beyond the fleet tier.

Kimi mechanics (verified): server-side prefix-hash cache; cache_control
markers and breakpoint positions are IGNORED; retention marker honored
(~1h TTL); reuse = longest byte-identical prefix, account-scoped. The only
lever is how much payload is byte-stable, ordered head-first.

## Design: add Tier 1 (project charter) to the prefix stack

| Tier | Content | Shared by | Today |
|---|---|---|---|
| 0 | pi system + skills + doctrine head | fleet / stack | ✅ |
| **1** | **project charter: repo layout, canonical commands, conventions, key-file map** | **all beads of the project** | ❌ this PRD |
| 2 | chain bundle (frozen PRD/design slice) | all phases of one chain | ❌ future |
| 3 | per-bead title/phase/memories | bead + retries | ✅ tail |

### Charter lifecycle

1. **Generation** (deterministic): daemon generates the charter at campaign
   start from a repo scan (top-level layout, package manifests, build/test
   commands, key source dirs) + the PRD's conventions section. No LLM in
   the generation path — deterministic output, byte-stable by construction.
2. **Placement**: after the doctrine head, before all per-bead content —
   so every bead of the project shares the prefix through the charter.
3. **Freeze**: versioned per campaign; charter hash logged per spawn
   (extends the existing prompt_sha256 log); changes mid-campaign are
   deliberate (version bump), never ambient.
4. **Consumption**: buildPiPrompt embeds the charter block from a path in
   the worktree (e.g. docs/hydra/charter.md, committed by the daemon at
   campaign creation — same integration path as handoff artifacts).

### FR-1.5 discipline (the blast-radius rule)

A volatile leak into Tier 1 poisons every bead of the project. Mandatory:
- no timestamps, worker/bead/run IDs, hostnames, or random values in the
  charter (enforced by the same golden-test approach as #228)
- canonical sorted serialization of any generated lists
- golden-file test: charter byte-identical across regeneration from the
  same repo state

## Goals / non-goals

| | |
|---|---|
| G1 | Discovery-free first turns for beads 2..N of a project (cache hit through the charter region) |
| G2 | Charter doubles as on-disk project context for humans (inspectable, diffable) |
| NG1 | Fleet-wide skill pinning for cache (trades worker quality — rejected) |
| NG2 | cache_control marker games (Kimi ignores markers — verified) |
| NG3 | Cross-bead transcript sharing (that is the gated resume pilot, PRD self-healing-context-recovery) |

## Success criteria

| # | Criterion | Target |
|---|---|---|
| SC1 | head8k_sha256 identical across two same-project beads differing only in per-bead content | test + live prompt_sha256 logs |
| SC2 | Discovery-class beads: fresh-input reduction vs 2026-09-15 baseline wave | ≥60% |
| SC3 | Charter regeneration from identical repo state is byte-identical | golden test, CI |
| SC4 | No verified-close rejection regression on charter-carrying beads | parity with pre-charter rate |

## Risks (quality-first)

| Risk | Mitigation |
|---|---|
| Stale charter mid-campaign (repo drifts) | frozen per campaign; hash logged; deliberate version bumps only |
| Volatile leak into tier 1 | FR-1.5 golden tests + prefix-SHA drift alarm (existing log) |
| Charter wrong (generator misreads repo) | charter is advisory context, not instruction; gates unchanged; SC4 watches close quality |

## Phasing

| Phase | Scope |
|---|---|
| P1 | Deterministic charter generator + freeze/log + buildPiPrompt tier-1 block + golden tests |
| P2 | Charter auto-generation at campaign on boarding (hook into project registration) |
| P3 | Tier 2 (chain bundle) — same machinery, per-chain scope |

## References

- Run analysis: 2026-09-15 discovery wave (5× duplicated discovery)
- Kimi cache mechanics: CASS bullet (pi 0.83.0 audit) + pi-cache-optimizer
  shard measurements (91–97% intra-session hit rates)
- Adverse-effect analysis pattern: self-healing-context-recovery PRD
