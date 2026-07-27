# Model Allocation Doctrine

**Status:** decided 2026-07-27, evidence-based · **Supersedes** the earlier
sketch that put K3 on GREEN/FIX/REFACTOR

## The principle

> Hydra's value is that **superior decomposition lets cheap models succeed.**
> Intelligence is spent on the few decisions that shape everything; execution
> runs on the cheap model. If a build bead needs a stronger model, the
> decomposition failed — fix the PRD, not the model tier.

This is not a cost preference, it is the product thesis. If K3 were required to
execute beads, there would be no reason to run Hydra instead of pointing a
frontier model at the whole PRD.

## The evidence (our own runs, model held constant)

Every worker spawn to date — 177 across all scored runs — used
`kimi-for-coding` (K2.7). **K3 has never executed a single bead; per-phase
routing has never fired.** So this entire improvement curve was achieved on the
cheap model:

| Run | Fix-beads / total beads | One-shot rate |
|-----|------------------------|---------------|
| dogfood-7a | 27 / 36 | 0 accepts (gate bug) |
| A2 | 26 / 36 | 1.0 |
| A (gap-to-prd) | 3 / 13 | 1.0 |
| B (security-scan) | 4 / 25 | 1.0 |
| C (observer) | **0 / 31** | 1.0 |
| D (prd-lint) | 11 / 64 (gap-cycle tail) | 1.0 |

**One-shot rate was 1.0 in every run** — every accepted bead passed validation
on the first attempt. There is no quality headroom for a stronger model on the
beads that pass.

The failure taxonomy is equally clear. This month's failures were: a gate bug
looking at the wrong directory (machinery), "consultant mode" from a prompt
regression (doctrine), orphaned `in_progress` beads after a daemon restart
(operator error), quota exhaustion (infrastructure), gap-cycle churn (analyzer
+ gap-to-prd quality), and wiring/glue beads (PRD atom-sizing). **None was
"the model was not capable enough."**

## Allocation

Spend intelligence where **volume is low and the consequence is irreversible**.

| Decision point | Volume | Model | Rationale |
|---|---|---|---|
| PRD authoring (Claude Code) | 1 / feature | Opus/Fable | Compiler input; a vague PRD cannot be rescued downstream |
| PRD authoring (on the VPS) | 1 / feature | `k3-256k` @ `max` via `scripts/prd-author.sh` | Same leverage, no Claude Code on the box; user-initiated only |
| Task decomposition | 1 / PRD | `claude-opus` (`task-breakdown/cli.mjs`) | Bead sizing decides whether cheap execution can succeed |
| **Gap adjudication** | ~dozens / cycle | **`k3-256k` @ `max`** | A wrong verdict drops a real defect or spawns a wasted chain |
| Observer diagnosis (Phase 1b) | ~few / cycle | `k3-256k` | A wrong root cause wastes an entire run |
| RED / GREEN / REFACTOR / SYNTAX / DISCOVERY | **hundreds / run** | **`kimi-for-coding` (K2.7)** | Empirically one-shot at 1.0; this is the thesis |
| FIX on retry ≥ 2 | rare | optional escalation | Escape hatch, never the default |

**Not used:** `kimi-for-coding-highspeed` (3× quota for speed we do not need)
and `k3` 1M (2× the quota of `k3-256k` for context no bead approaches).

## Two constraints that follow from the endpoint

1. **The reasoning dial only exists on K3.** K2.7 exposes `Thinking` as a
   binary on/off; only `k3`/`k3-256k` accept `reasoning_effort:
   low|high|max` (default `high`; `medium` maps to `high`). On a pure-K2.7
   build fleet, per-phase thinking levels are a **no-op** — which is why the
   phase-level half of `docs/PRDs/phase-thinking-routing-20260727.md` is
   deferred and only its adjudicator half shipped.
2. **Never disable thinking.** Turning thinking off silently reroutes K3/K2.7
   requests to the older K2.6 model — a quality downgrade disguised as a
   saving. `normalizeThinking` coerces `none`/`off`/`disabled` to `low`, and
   `scripts/prd-author.sh` refuses those values outright.

## Where the quota actually goes

The 2026-07-27 exhaustion was **pure K2.7 volume**, not model tier: **804
worker spawns in four days** (4156 all-time), each carrying the ~4.9KB system
prompt plus bead context, CASS rules, Hindsight recall, file reads, and up to
four retry attempts.

So the levers that matter are volume levers, not model levers:

- **Do not retry unretryable failures.** Quota exhaustion was retried 4× per
  bead *and* misclassified as a code failure, spawning a fix bead per bead —
  each of which retried again. The failure consumed the quota it was failing
  on. Fixed: `isQuotaError` now drives both `shouldRetry` and
  `classifyFailure` (exit code 2 = infrastructure, no fix bead).
- **Shrink repeated input**, not output. The system prompt is paid on every
  spawn and every retry; agent prose is a rounding error by comparison. This
  is also why output-compression (caveman-style) is *not* applied to build
  workers: it would trade a ~1–3% saving against the precision the verified-
  close gates depend on. Terse-instruction belongs on the adjudicator and
  future Observer triage, where output is parsed as JSON anyway.
- **Cut gap-cycle churn** (run D: 13 original beads → 64 total) via better
  analyzer precision and adjudication quality — which is exactly what the
  `k3-256k` adjudicator buys.
