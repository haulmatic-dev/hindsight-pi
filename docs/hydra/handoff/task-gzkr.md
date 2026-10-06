# discovery handoff (hydra-gzkr)

## What changed (files + one line each)
- docs/discovery/orphanclaim-publisher-disk-triage-gzkr.md: triage — all three P0 claims in the fleet snapshot verified stale; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.
- Live-store action: twin pan-filed bead hydra-lg7b — triage comment added + closed as duplicate of hydra-gzkr ("not_planned: duplicate of hydra-gzkr; all claims stale"). br WAL poison at bead start cleared via `br doctor migrate-schema recover`.

## Verified findings
- Orphans: registry has only 2 `working` rows (gzkr, n9nq, fresh heartbeats); 20 in_progress beads in the hydra store — every non-live one carries a fresh `completed` registry row with exit 0 (bldo 13:32:07Z … cw35 12:35:23Z). Workers spawn+finish; retry queue live. Charon: 47 `charon_orphan_reopened` (dead_worker) today, latest 13:27:33Z (hydra-gcal). Digest: 0 "P0 campaign anomaly" filings today. Fleet progressed (14 `bead_closed` today). Known churn: cw35/bldo/95ia `verified-close-reject-gate-*` retry loop; dyw2 closes hit `verification_command_missing` (fail-open, legacy bead) and stay in_progress — bounded by retry ladder, hu12 disposition stands. NOT a scheduler bug.
- Publisher: local checkout 0 commits ahead of origin/development; PR #263 MERGED 2026-10-06T13:25:30Z, mergeCommit e5e33b5 == `git ls-remote origin development` tip. Remaining open PRs (#228–#262) are per-bead dogfood PRs, not the publish channel. No human merge pending.
- Disk: `df -h /` → 212G/451G = 50% (watermark 85%); `disk_cleanup_run` ×402 today (cooldown as designed). No ENOSPC risk.

## Verification (commands run + pass/fail)
- `sqlite3 registry.db` worker queries — 2 working fresh, 10 completed rows fresh exit 0. PASS.
- `br list --status in_progress` (post-WAL-recovery) — 20 beads, labels enumerated. PASS.
- Event scan 2026-10-06.jsonl — 47 charon reopens, 0 anomaly filings, 14 closes, counts per type tabulated. PASS.
- `git rev-list --count origin/development..HEAD` = 0; `gh pr view 263` → MERGED e5e33b5 @13:25:30Z; `git ls-remote origin development` = e5e33b5. PASS.
- `df -h /` → 50%. PASS (claim stale).
- hindsight_search at bead start — not connected (skip rule applied). PASS.
- graphify-out/graph.json absent in cwd → rg/registry/event fallback per protocol. PASS.

## Open edges for the next phase
- Churn cost persists (cw35/bldo/95ia/dyw2 burn one worker per retry; ~10-min Charon reopen cycle). A future bead could make `verification_command_missing` on legacy beads a loud one-time flag rather than fail-open every attempt, or bound reopen churn per bead per day.
- If a push-pending pseudo-bead re-fires: local checkout is 3 behind origin/development; daemon publish loop fast-forwards — no operator action.
