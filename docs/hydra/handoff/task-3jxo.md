# discovery handoff (hydra-3jxo)

## What changed (files + one line each)
- docs/discovery/orphanclaim-publisher-disk-triage-3jxo.md: triage — all three P0 claims verified stale/known-class; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verified findings
- Store needed `br doctor migrate-schema recover` at bead start (WAL poison, "database is busy (recovery in progress)"); cleared, then `br list --status in_progress` → 22 beads.
- Registry cross-check: 15 in_progress beads with completed rows, heartbeats 5–27 min stale (retry-gap churn, hydra-hu12 class); 7 live working rows; 5 live worker processes confirmed by ps. Charon sweep LIVE: scans=90, orphans_found=0, 48 charon_orphan_reopened today (latest 13:34:36). q207/rxi6 escalated → orphan_escalated_wedged by design. 0 "P0 campaign anomaly" filings today. Manual reap NOT executed per playbook (recovery operating, cohort transient).
- Publisher wedge: `git merge-base --is-ancestor HEAD origin/development` → YES; remote tip c911a40 (PR #266 merge). No unpublished local work; open PRs are pan-filed proposal PRs, not publish wedges. Local checkout behind 6 — daemon fast-forwards. Main checkout has another strand's uncommitted changes; untouched.
- Disk: `df -h /` → 214G/451G = 50% (watermark 85%) — claim stale. `systemd-tmpfiles --clean` executed as hygiene (benign permission warnings only).

## Open edges for the next phase
- Churn cost persists: gate-failed cohort burns a worker per retry (see hu12 open edge); reduction belongs to gate-failure root-cause beads, not orphan recovery.
- q207/rxi6 await human/Pan clear-escalation.
- If anomaly re-fires, first check deployed binary mtime vs origin/development tip (recurring stale-binary pattern, 5fbq/pf9c).
