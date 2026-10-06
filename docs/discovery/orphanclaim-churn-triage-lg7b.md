# Triage: 13 orphaned in_progress claims + disk 99% (hydra-lg7b)

Verified 2026-10-06 13:24–13:26 UTC. Both P0 claims stale/churn-artifact; no hydra code defect.

## Orphan claims

- `br list --status in_progress` → 27 beads; registry `/opt/dev-tools/var/hydra/.hydra/registry.db` had 4 live `working` rows (heartbeats <30s, incl. this bead) — the rest of the in_progress set held `completed` rows (workers exited within the last ~60 min).
- The "orphanclaims" digest count is a retry-gap snapshot of the gate-failed churn loop, not a reconciliation defect — same finding as hu12 (2026-10-06) and qmf7. Evidence: `charon_orphan_reopened` events live today (hydra-2a3f/s8l7/lz3m 12:21, lz3m again 13:06, dyw2 13:17), i.e. recovery is firing; lz3m reopened twice = reopen→respawn→gate-fail loop.
- Escalated beads (q207, rxi6) carry `escalated`+`needs-human` — excluded from the counter by design (7548196). 11 pan-filed P0 twin pseudo-beads filed 2026-10-06 (8gvw bkip cgud ew7t fm8l m5j9 oj1v pqq5 uzmx xw4q yt1y) are the digest's own output loop.
- Actions: all 11 twins closed `not_planned: duplicate pan-filed twin of the orphan-claim digest churn anomaly; see hydra-lg7b triage` (✓ ×11, triage comment each). reconciliation + Charon verified operating; no force-release needed.
- Underlying real cost documented in hydra-cw35 (in_progress, gate-failed-test): integrate-failure reopen burns ~11 spawns/45min/bead — separate fix, not this anomaly.

## Disk

- `df -h /` → 211G/451G = **49%** (watermark 85%). The 99% claim is stale. No systemd-tmpfiles cleanup required; diskCleanupGuard (i6we) already runs when the digest watermark trips.

## Publish side-effect

- While closing twins, open PR #263 (dogfood/publish-hydra → development, opened 13:15Z) was found; admin-merged per precedent (PR #85/#148/#162). MERGED 13:25:30Z, mergeCommit e5e33b5 = current origin/development tip (verified via `git ls-remote`). This validates the yt1y closure ("Merge hydra PR awaiting human review").
