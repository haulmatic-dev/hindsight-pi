# Roadmap: Self-Improvement Loop

**Status:** planned · **Owner:** Hydra core · **Created:** 2026-07-22

## Vision

Hydra continuously observes its own operation, diagnoses defects and
regressions, and drives the fix through its own dogfood pipeline — detect →
diagnose → propose (PRD) → build (dogfood) → PR → (gated) deploy. The loop that
several 2026-07 sessions ran *by hand* (e.g. finding the verified-close
"empty-diff" gate bug via log forensics) becomes a standing, automated
capability.

**Guiding principle:** value is front-loaded and risk is gated. Full autonomy
(auto-fix + auto-deploy) is the last and riskiest step, not the first. We
automate observation and diagnosis fully; we gate action progressively, only
after the earlier phases prove a low false-positive rate.

## Why now

The building blocks already exist:
- `tools/hydra-eval` — run metrics (accepts, one-shot rate, stuck kills, …).
- Global daemon log + per-bead worker logs + (planned) per-repo activity log.
- `eval/run.sh` — one-command scored dogfood.
- `create-prd` skill with mandatory adversarial review; scope gate; verified
  close; worktree isolation.

The cron is mostly glue over these. Two live false-positive incidents
(BLOCKED-homes cry-wolf; empty-diff mis-attributed to "consultant" workers)
are the evidence for gating action behind verification.

## Phases

### Phase 1 — Observer (safe; ~80% of the value; build first)
A timer (systemd, ~10–15 min) that:
1. Runs cheap checks: `hydra-eval` over recent runs + scans logs for known
   signatures (empty-diff spikes, stuck-worker kills, panics, `BLOCKED`,
   provider/auth errors, activity-timeouts).
2. A **cheap model** (e.g. Kimi low-effort / Haiku) triages against a baseline:
   anomaly or not?
3. On anomaly, escalates to a **strong model** for root-cause diagnosis.
4. Writes a ranked **backlog entry + draft PRD** to a queue. **No production
   mutation.** Dedupes against open issues so one defect ≠ N entries.

**Exit signal:** would have flagged the empty-diff gate bug within one interval.
Measure its false-positive rate here before granting later phases the right to
act.

### Phase 2 — Proposer (gated)
For high-confidence, dogfood-shaped issues: auto-generate a PRD (via `create-prd`
+ adversarial review), auto-run a dogfood on a branch, and open a **draft PR
with before/after metrics**. Human merge remains the gate. This automates
exactly the manual loop used today.

### Phase 3 — Actor (tightly gated; last)
Auto-apply only a **whitelist** of low-risk fixes (config toggles, log-noise,
docs) behind: tests green + dogfood accept-rate not regressed + **auto-rollback
on health-check failure**. Risky core changes stay draft-PR for human merge.

## Safety invariants (all phases)

- **Separation:** the fixer/supervisor runs as a **separate unit** from the
  daemon it watches, so it can restart/roll back a broken daemon and a bad
  self-deploy cannot brick the fixer.
- **Confirm before act:** no fix without a verified reproduction + passing
  tests; no deploy without a health gate + automatic rollback.
- **Human merge stays the gate** for the `development` branch. Auto-deploy is
  canary-with-rollback only; production defaults to merged `development`. An
  improved branch may be deployed ahead of merge, but only under the health
  gate.
- **Budget + dedupe:** per-cycle token cap; one issue → one backlog item.
- **Kill switch** + full audit logging of every detection, diagnosis, and
  action.

## Deliverables & dogfooding

| Phase | Deliverable | Dogfoodable? |
|-------|-------------|--------------|
| 1 | `tools/hydra-observer` (Go analyzer) + diagnosis prompt + systemd timer + backlog writer | **Yes** — Go tool + prompt; the analyzer/backlog core is a clean dogfood PRD (timer install is ops) |
| 2 | Orchestration wiring PRD→dogfood→draft-PR | Partly (Go orchestration dogfoodable; git/PR + systemd are ops) |
| 3 | Whitelisted auto-apply + supervisor/rollback unit | Mostly ops (supervisor, health gates, rollback) — spec-and-review heavy, light on dogfood |

**Plan:** dogfood Phase 1's analyzer + backlog core; measure false-positive
rate; then gate Phases 2–3 on that signal quality.

## Relationship to other work

- Extends `tools/hydra-eval` (WO-E) — the metrics half becomes the Observer's
  input.
- Consumes the per-repo activity log (see `docs/PRDs/publishable-observable-output-20260722.md`).
- Complements the master plan in `docs/hydra-analysis-20260712iv.md`.
