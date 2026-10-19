# discovery handoff (hydra-284j)

## What changed (files + one line each)
- docs/discovery/orphanclaim-churn-triage-284j.md: triage — all three P0 claims (orphaned in_progress claims, publisher wedge, disk 99%) verified stale; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verification (commands run + pass/fail)
- `df -h /` → 169G/451G = **40%** (watermark 85%) — disk-99% claim stale. PASS.
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` — exactly 1 `working` row (hydra-284j, this bead, heartbeat fresh); all other rows `completed` with heartbeats 12:22–14:57 UTC, latest cohort within the same minute. 0 live orphans. PASS.
- `br list --status in_progress` — 18 in_progress: 6 pan-filed meta twins (excluded by 7ef09f6), 2 escalated human-gated (q207, rxi6; excluded by 7548196), 10 gate-failed/discovery churn candidates with fresh completed registry rows. 0 wedged orphans. PASS.
- Events 2026-10-06.jsonl — `charon_orphan_reopened` (dead_worker) hydra-dyw2 14:48:30Z, hydra-7tzf 14:49:30Z; zero "P0 campaign anomaly" filings matched. Recovery live. PASS.
- Publisher wedge: local checkout `git rev-list --count origin/development..HEAD` = **0**; origin/development tip c3be2cf (PR #265). No open dogfood/publish-hydra PR (open PRs #271, #223–#234 are per-bead dogfood strands). Wedge absent. PASS.
- `br doctor migrate-schema recover` — cleared recurring WAL poison ("database is busy (recovery in progress)"). PASS.
- hindsight_search at bead start — not connected (skip rule applied). graphify-out/graph.json absent in cwd → registry/br/event fallback per protocol. PASS.

## Open edges for the next phase
- Hygiene only: pan-filed twin cohort (o331, m3bj, z0do, 6t5v, rz63) closeable as duplicates once their workers exit — do NOT force-release claims.
- Open dogfood PRs #223–#234 (validation/refactor strands from 10-04) await human review; not part of this anomaly.
