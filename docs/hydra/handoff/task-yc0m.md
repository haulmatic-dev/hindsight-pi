# discovery handoff (hydra-yc0m)

## What changed (files + one line each)
- docs/discovery/p0-anomaly-triage-yc0m.md: triage — all three P0 campaign claims verified stale/resolved; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verified findings
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 6 live `working` rows (rhin, y4i4, n9nq, faeu, yc0m=self, 4p4d), fresh heartbeats. Orphan cohort = escalated (q207, rxi6 — excluded per 7548196) + pan-filed meta twins (xcye, 3jxo, 6dbv, rz63 — excluded per 7ef09f6) + queued discovery beads (transient). 0 true orphans. PASS.
- Charon recovery LIVE: 48 `charon_orphan_reopened` (dead_worker) events in 2026-10-06.jsonl, latest 13:34 UTC (m72l). Zero "P0 campaign anomaly" filings today. PASS.
- Publisher wedge STALE: `git rev-list --left-right --count origin/development...HEAD` on /opt/dev-tools/components/hydra → `7 0` (local behind, 0 ahead — nothing unpublished). origin/development tip 53eca1b = "Merge pull request #268 from haulmatic-devteams/dogfood/publish-hydra"; publish merges #263/#264/#266/#268 all landed today. Publish path healthy; no human merge pending. PASS.
- Disk: `df -h /` → 215G/451G = **50%**, below 85% watermark. 99% claim stale; no cleanup needed. PASS.

## Open edges for the next phase
- None for the P0 claims. Many open dogfood PRs (#208–#267) are normal bead PRs awaiting integration — not this bead's publish wedge; do not admin-merge them reflexively.
- Recurring stale-snapshot digest filings remain the pattern for this anomaly class; if it re-fires, check binary staleness first (playbook per 8gvw).
