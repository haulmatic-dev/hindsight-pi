# RTK End-to-End Evaluation — Decision Record

Status: **parked → decided (keep, reconfigure follow-up)** · 2026-09-22
Author: Hydra dogfood run (bead: RTK end-to-end evaluation)

## Question

GitHub measured tool-output compression *increasing* end-to-end turns/cost:
omitted text matters → model re-opens the original output or reruns the
command, adding recovery turns. Hydra runs `pi-rtk-optimizer` in every
worker's extension allowlist and had never measured it end-to-end. Decide:
keep / reconfigure / remove.

## Evidence

### 1. Controlled micro A/B (2026-09-21, rpc-mode, 2 tasks × 2 arms)

| arm | turns | output tokens | gates |
|---|---|---|---|
| rtk | 6, 5 | parity | all pass |
| control | 4, 5 | parity | all pass |

Verdict: **underpowered** — only 1 compaction event across 4 runs, no
statistical significance. Directionally negative on turns, consistent with
GitHub's finding, but unusable as a decision basis.

### 2. Fleet transcript analysis (996 pi session files under
`/opt/dev-tools/var/hydra/sessions/`, parsed with the same markers as
`scripts/turn_economy.py`; RTK marker `[RTK compacted output`)

- Compactions: **1,602–1,613** events across the fleet period (Aug–Sep 2026).
- Re-opens (successful later read of the same path after its output was
  compacted): **551** matched events — **34%** of compactions later re-opened;
  78% of compacted *paths* were read again at least once.
- **Recovery cost per re-open**: turns from compaction to re-open
  p25/p50/p75 = 1/2/7, mean 13.3. 235/551 (43%) re-open within the same or
  next turn (cheap). A heavy tail (mean ≫ median) means a minority of
  re-opens cost dozens of turns — total ≈ 7,300 recovery turns fleet-wide.
- Cross-sectional confound: sessions with any rtk activity are longer
  (reopen cohort turns p50 89 vs 25 for no-rtk sessions). Session length
  drives both compaction count and turns, so the raw delta (+78 turns) is
  **not** attributable to rtk. It bounds nothing; only the within-session
  compaction→reopen gap is causal-ish.

### 3. What gets re-opened (reconfigure signal)

Top re-opened paths are **repeatedly-edited working files**, not one-shot
dumps: test spec files (`permission-template.service.spec.ts` 59×,
`collaborator-invitation.service.spec.ts` 48×), hydra source
(`internal/core/config.go` 34×, `bead_closer.go` 24×), `package.json` 34×,
PRDs and skill files. Compacting a file the worker is about to edit/re-read
is the exact failure mode: the content matters, so the model pays a
recovery turn to get it back.

RTK config (`config/pi/extensions/pi-rtk-optimizer/config.json`) already has
the knobs: `outputCompaction.sourceCodeFiltering: "minimal"` compacts source
reads; `preserveExactSkillReads: false` compacts skill reads (a skill file
was compacted 31× and re-opened); `smartTruncate.maxLines: 220` /
`truncate.maxChars: 12000` set the compaction floor.

### 4. Savings side

Unmeasured. `trackSavings: true` is set but no savings aggregate was found
in worker logs or the rtk `history.db` (which only covers the CLI-rewrite
hook, 14 rows). Input-token savings from 1,600 compactions are real but
unknown; `cacheRead` dominates input cost, so cash savings are likely
smaller than raw token counts suggest.

## Decision

**KEEP rtk in the fleet allowlist; do not remove.** Removal would forfeit
savings on genuinely large one-shot outputs (test-runner dumps, build logs,
grep floods) where no recovery occurs, based on an underpowered A/B and a
confounded observational delta.

**RECONFIGURE as the follow-up** (targeted, evidence-backed):

1. `preserveExactSkillReads: true` — skill/PRD files are reference material
   that workers cite verbatim; compacting them produced re-opens with zero
   offsetting benefit.
2. Raise the compaction floor (`smartTruncate.maxLines`, `truncate.maxChars`)
   so moderate working files (specs, `config.go`-sized sources) pass through
   intact; reserve compaction for outputs ≫ the re-read cost.
3. Do not compact read-tool results for paths the session has already
   read/edited more than once (needs an rtk feature or upstream issue — the
   current knobs can't express it).

## Follow-up: two-window fleet A/B (the powered version of the micro A/B)

The micro A/B failed on sample size; the fleet gives size but no control
arm. Procedure once a control window is available:

1. Window A (rtk on, current fleet default) vs Window B (rtk dropped from
   `agents.pi.extensions` / `phase_extensions` — edit
   `/etc/hydra/hydra.yaml`, restart daemon). Same bead mix, ≥2 weeks each,
   ≥200 sessions per window.
2. Compare with `scripts/turn_economy.py` per window:
   turns p50, cmd-rerun rate, repeat-read rate, `rtk_compacted`,
   `rtk_reopens`, guardrails (gate first-try, empty-patch).
3. Decision rule: remove rtk if Window B shows turns p50 lower by >10%
   *with no guardrail regression*; keep the reconfigure items above
   regardless of outcome, since §3 is path-specific waste independent of
   the overall sign.

## Reproduce the fleet numbers

```bash
python3 scripts/turn_economy.py \
  --sessions-dir /opt/dev-tools/var/hydra/sessions \
  --out-json /tmp/rtk-eval.json --out-md /tmp/rtk-eval.md
# per-event compaction→reopen gap: parse session jsonl for the
# "[RTK compacted output" marker, then match later successful read of the
# same path (same logic as turn_economy.py lines ~250-270)
```
