# discovery handoff (xw4q)

## What changed (files + one line each)
- docs/discovery/orphanclaim-churn-triage-xw4q.md: triage — orphanclaims=14 is a pre-restart churn snapshot, not a wedge; reconciler and Charon verified live; disk 49% (stale claim), systemd-tmpfiles cleanup run. No code change.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verification (commands run + pass/fail)
- `df -h /` → 210G/451G = 49% (watermark 85%) — 99% claim stale. PASS.
- `br doctor migrate-schema recover` — cleared recurring WAL poison ("database is busy (recovery in progress)"). PASS.
- `br list --status in_progress --json` + registry.db cross-check — 25 in_progress: 9 live workers (heartbeats <60s), 9 pan-filed meta (excluded by design), 2 escalated human-gated (q207, rxi6; excluded by design), 6 churn candidates with completed registry rows (cw35/bldo/95ia/m3au/m72l/gcal) being actively retried (gate_run 12:51–12:52). 0 live orphans. PASS.
- events 2026-10-06.jsonl — charon_orphan_reopened (dead_worker) ×10+ today incl. post-restart; zero "P0 campaign anomaly" filings after 12:16 daemon restart. PASS.
- `systemd-tmpfiles --clean` — executed (expected permission-denied noise on other-user /tmp dirs). PASS.
- hindsight_search at bead start — not connected (skip rule applied). PASS.
- graphify-out/graph.json absent in cwd → rg + live-store fallback per protocol. PASS.

## Open edges for the next phase
- Human review: q207, rxi6 (escalated, awaiting judgment; Pan clear-escalation = one fair retry).
- Gate-failure churn cohort (cw35/bldo/95ia/m3au/m72l/gcal): verify the durable escalation ladder actually caps them; if loop persists past cap, next triage targets the ladder, not reconciliation.
