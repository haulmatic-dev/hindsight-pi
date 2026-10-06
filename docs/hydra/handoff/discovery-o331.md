# discovery handoff (hydra-o331)

## What changed (files + one line each)
- docs/discovery/orphanclaim-publisher-disk-triage-o331.md: triage — all three P0 claims (14 orphaned in_progress claims, publisher hold awaiting human merge, disk 99%) verified stale/false at 14:21 UTC; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verification (commands run + pass/fail)
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` cross-checked with `br list --status in_progress` (21 beads): 4 live `working` rows heartbeats <60s (n9nq, o331=self, xcye, z0do); every other in_progress bead has a `completed` row with heartbeat 5–60 min old (retry-gap churn); q207/rxi6 `escalated`+`needs-human` excluded by design (7548196). 0 true orphans. PASS.
- `grep -c "P0 campaign anomaly" events/2026-10-06.jsonl` = 0 — digest quiet today. PASS.
- Charon live: `charon_orphan_reopened` (dead_worker) at bead-start instant (ngui 14:21:32Z, 96th 14:21:32Z). Reconciliation operating end to end; deployed binary built 12:16 UTC today. PASS.
- Publisher: `git rev-list --count origin/development..HEAD` (local checkout) = 0; `git ls-remote origin development` = 5b6f28b0 = PR #270 merge commit; `gh pr list --state open` has NO dogfood/publish-hydra PR. Wedge absent, no human merge pending. PASS.
- Disk: `df -h /` → 167G/451G = **39%** (watermark 85%) — 99% claim stale. `systemd-tmpfiles --clean` run (rc=0). diskCleanupGuard (a4d40f4) already covers real breaches. PASS.
- hindsight_search at bead start — not connected (skip rule applied). PASS.
- graphify-out/graph.json absent in cwd → registry/br/event/live-checkout evidence per protocol. PASS.

## Open edges for the next phase
- Human/Pan decision on hydra-q207 + hydra-rxi6 (escalated since 10-04): Pan `repair:clear-escalation` for one fair retry, or abandon-with-summary. Only true human-gated items.
- Recurring digest churn twins (this bead is one more instance of the 0ndi/8gvw/bkip/gzkr/lg7b class) — landed exclusions have kept today's filings at 0; no further code change indicated.
