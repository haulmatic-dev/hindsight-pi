# Turn-Economy Measurement Harness

`scripts/turn_economy.py` is the productized replacement for the ad-hoc Python
forensics previously hand-run over pi session transcripts. It reads
`{sessions-dir}/<bead-id>/*.jsonl` (same layout `cmd/worker/session_stats.go`
consumes) and emits per-bead, per-chain, and per-campaign aggregates as JSON
and a markdown summary. Exit code 2 when a guardrail regresses; 1 on
usage/IO errors; 0 otherwise. Fail-open on malformed lines and unreadable
files (it is daemon telemetry, not a product gate).

## How to run

```bash
python3 scripts/turn_economy.py \
  --sessions-dir /opt/dev-tools/var/hydra/sessions \
  --campaign pre1=2026-08-01..2026-08-20 \
  --campaign pre2=2026-08-20..2026-09-01 \
  --campaign post1=2026-09-01..2026-09-11 \
  --campaign post2=2026-09-11..2026-09-22 \
  --compare-before pre1,pre2 --compare-after post1,post2 \
  --out-json /tmp/te-report.json --out-md /tmp/te-report.md
```

- `--campaign NAME=START..END` — ISO-8601 timestamp window over each
  session's start timestamp; bounds compare lexicographically, either bound
  may be empty. With no `--campaign`, everything lands in one `all` cohort.
- `--compare-before/--compare-after` — comma-separated campaign names whose
  merged cohorts get a before/after block with guardrail checks.
- `--gate-cmds` — JSON array of gate command templates, same convention as
  `HYDRA_GATE_CMDS` in `cmd/worker/session_stats.go` (that env var is used
  when the flag is absent). Prefixes are the command up to the first
  flag/placeholder token, mirroring `gateCmdPrefix`.
- Unit tests: `python3 -m unittest discover -s scripts/tests -v` (stdlib
  `unittest`, no pytest dependency). Fixtures are generated in tmp dirs.

## What it measures

- **turns / tokens** — assistant-message count; sums of `usage.input`,
  `output`, `cacheRead`, `cacheWrite`. Input sums overstate cost under
  provider caching; `cacheRead` dominates and is the cheap tier.
- **zero-action turn rate** — assistant turns with no `toolCall` / total.
- **repeat-read rate** — per chain (sessions grouped by normalized
  chain subject from the first user message), share of reads whose path was
  read more than once.
- **cmd-rerun rate** — share of bash invocations (whitespace-normalized)
  run more than once per chain. Together with repeat-read rate this is the
  RTK keep/fix/remove signal. Decision record and fleet evidence:
  `docs/operations/rtk-evaluation.md`.
- **RTK recovery events** — `rtk_compacted` counts tool results carrying the
  `[RTK compacted output` marker; `rtk_reopens` counts a later successful
  re-read of the same path after compaction (the "full-output re-open"
  recovery pattern).
- **thinking/answer split** — character split between `thinking` and `text`
  blocks (a proxy for the token split; per-block token counts are not in the
  transcript).
- **first-8 tool-mix histogram** — toolCall counts within the first 8
  assistant turns; an orientation-waste measure.
- **per-phase distributions** — RED/GREEN/REFACTOR/SYNTAX/GATE parsed from
  the `task: <PHASE>:` first line or the Context `task.phase` field;
  p25/p50/p75/max of turns. These constants are what any future turn cap
  needs.
- **target-files cohort** — sessions whose injected Context block declares
  `target.files` vs not; the cohort repeatedly shows far fewer turns
  (prototype: 20 vs 68 p50), confirming scoped beads converge faster.

## How to read the guardrails

Reported per campaign and checked in the before/after block:

- **gate first-try pass rate** — of sessions that ran a gate command, share
  where every gate prefix's *first* attempt succeeded (a failure-then-pass
  sequence counts as not first-try). Must not **fall** after a turn-economy
  change; a fall means workers are rushing and letting gates catch errors.
- **empty-patch rate** — share of sessions with zero successful edit/write
  tool results. Must not **rise**; a rise means more beads burn turns
  without producing a diff.

Either regression blocks a "success" claim: the markdown banner lists the
regressions and the process exits 2, so CI/wrappers can gate on it. Guardrail
denominators (`gate_sessions`, `sessions`) are printed next to every rate —
always check them before trusting a rate over a small n.

## Reference numbers (2026-09-21, 970 sessions, pre/post context-pack split)

Before cohort (pre1+pre2, Aug): turns p50 69, zero-action 0.29, repeat-read
0.67. After cohort (post1+post2, Sep): turns p50 36, zero-action 0.21,
repeat-read 0.49. Per-phase p50 post-pack: RED 33, GREEN 39, REFACTOR 24,
SYNTAX 23, DISCOVERY 8. Guardrails: no regression detected. Regenerate with
the command above; do not quote stale numbers.
