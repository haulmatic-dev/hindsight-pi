# Self-healing context recovery: resume-in-place options, quality gates (idea PRD)

Status: analysis complete, quality-gated, queued for future build (beads
referenced below). Quality is the #1 acceptance criterion for every item;
cost is #2. Do not implement any item without its gates.
Date: 2026-09-15

## Problem

The orphan-recovery self-healing path (Charon detect → reopen → respawn) is
correctness-first and cost-blind: the new worker gets a fresh worktree,
fresh pi session, and re-buys all discovery context the dead worker already
paid for. This PRD records the options to recover that context, the
adverse-effect analysis that gates them, and the build order.

## Doctrine restatement (required by the analysis)

Strict retry determinism (original SC3, "retried phase sees byte-identical
context") **conflicts with context reuse**. Restated rule:

> Determinism applies to gates and artifacts, not to context reuse.
> Close-path gates (verified-close, scope, changelog) must always assume
> fresh-attempt semantics and never trust inherited context.

## Options and verdicts

| # | Option | Value | Verdict |
|---|---|---|---|
| E | Worktree preservation at orphan recovery | Moderate | **Build first — with WIP gate** |
| D | Pre-death handoff flush | Moderate | **Build second — with distrust labeling** |
| B | Deterministic respawn prompt (pinned extras) | Small | **Build third — narrow scope** |
| A | Session-file resume on respawn | Highest | **Pilot behind strict gates with quality-metrics auto-disable** |
| C | Per-bead account affinity | Cost-only | **Defer with P3 (pi extension)** |

### E. Worktree preservation + WIP gate (SAFE, build first)

Preserve the orphan's worktree at recovery **only if a cheap build/typecheck
gate passes** on the preserved state; otherwise discard and respawn
fresh-from-branch (today's behavior). "Preserve and pray" is net-negative:
half-applied WIP becomes the next worker's broken base, and preserved
experiments inflate diffs → scope-gate churn at close.

Gates: WIP gate green required; janitorial commit before respawn; handoff
artifacts carried via existing ChainContext loader.

### D. Pre-death handoff flush + distrust labeling (SAFE with labeling)

Worker-side death hook (kill signal / give-up): atomically write
`docs/hydra/handoff/<phase>.md` + `RESPAWN-NOTES.md` (tmp+rename), then exit
cleanly. **Adverse effect that mandates the labeling**: a worker dying
mid-confusion writes confident-but-wrong notes, and the current FROZEN CHAIN
CONTEXT prompt instructs the next worker to treat handoffs as verified and
NOT re-derive — errors would amplify under an authority instruction.

Gates (mandatory): death-hook handoffs are a **separate labeled class** —
"DIED MID-WORK — leads, not facts; re-run verification before building on
this"; distinct template; next-attempt prompt downgrades trust for that
class; evidence excerpts (last test output) instead of claims.

### B. Deterministic respawn prompt — narrow scope (LOW RISK)

Pin only *reference* extras (project-charter-style blocks) from attempt 1 so
the respawn's first request can hit the provider prefix cache. **Do not pin**
execution memories or hindsight recall — fresh recall is what surfaces
attempt 1's own recorded failure, the most relevant memory for attempt 2
(the reason the earlier pinning idea was rejected). Cache-key alignment must
be pinned by test to rule out cross-bead prompt contamination.

### A. Session-file resume on respawn — GATED PILOT (highest value, highest risk)

Pi persists sessions per-cwd; the orphan's session file + worktree exist at
recovery time, inside the cache-retention window. Resume recovers the whole
transcript at cache-read/input prices instead of re-deriving via new tool
calls.

Adverse effects that gate it:
- **Resuming the pathology** — wedged sessions (tool loops, poisoned context)
  re-import the disease; fresh start is a feature for these
- **Stale transcript vs reality** — "I edited X" no longer true; skipped
  verification, false carries-forward
- **Reflexion boundary erased** — model cannot tell attempts apart; may
  repeat the failed approach with more confidence
- **Zombie re-enactment** — respawn-in-place before Charon reaped the orphan
  row recreates the kv4 class

Mandatory gates: resume ONLY on infra-class death (never wedge/verdict-less
grind); attempt-boundary injection ("you are a NEW attempt; verify before
trusting prior claims"); worktree HEAD check against claimed state; session
age/size cap; strictly after orphan reaping.

**Quality-metrics pilot with auto-disable**: instrument fix-bead rate and
verified-close rejection rate on resumed vs fresh beads; if resumed beads
need more fixing, auto-disable resume. Ship the pilot dark (measure-only)
first.

### C. Per-bead account affinity — defer

Capability mismatch is the only quality risk (pin to a 16K-max-output
provider truncating long verdict turns → malformed HYDRA_VERDICT). Ship with
the P3 pi-extension work; `allow_fallbacks` always on; pin by model class.

## Build order (quality-first)

1. E + WIP gate
2. D + distrust labeling
3. B (narrow)
4. A pilot (dark → gated → measured)
5. C (with P3)

## References

- Adverse-effect analysis: session conversation 2026-09-15
- Pan/park work this builds on: hydra PR #232, llm-router PR #3
- Historical cautionary class: kv4 zombie incident (stale-state reuse
  without gates → 9 sequential BLOCKED states)
