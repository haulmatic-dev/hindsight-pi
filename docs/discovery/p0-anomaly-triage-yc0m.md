# P0 campaign anomaly triage — hydra-yc0m (2026-10-06 ~14:00 UTC)

All three P0 claims in the digest verified stale or already resolved. No code change.

## 1. "11 in_progress beads with no live registry worker"

- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 6 live `working` rows
  (hydra-rhin, y4i4, n9nq, faeu, yc0m=self, 4p4d), all freshly heartbeated.
- The in_progress cohort without rows decomposes into already-excluded classes:
  - `escalated` human-gated: hydra-q207, hydra-rxi6 (excluded per 7548196).
  - `pan-filed` meta twins: xcye, 3jxo, 6dbv, rz63 (+ this bead's own filings
    yc0m/4p4d/faeu now claimed — excluded per 7ef09f6).
  - discovery/queued beads (req-001..req-008) awaiting worker slots — transient.
  - Charon recovery LIVE: 48 `charon_orphan_reopened` (dead_worker) events
    today, latest 13:34 UTC (hydra-m72l); dyw2 13:17, gcal 13:27.
- Zero "P0 campaign anomaly" filings in events/2026-10-06.jsonl. The digest
  counter is quiet — the anomaly in the bead body is a stale snapshot, not a
  live condition. reconcileOrphanClaim + Charon + digest exclusions all deployed.

## 2. Publisher wedge ("locally-merged work unpublished 51m, PR awaiting human merge")

- `git rev-list --left-right --count origin/development...HEAD` on
  /opt/dev-tools/components/hydra → `7 0`: local checkout is BEHIND origin by 7,
  AHEAD by 0. No unpublished local work exists.
- The publish PR already merged upstream: origin/development tip 53eca1b =
  "Merge pull request #268 from haulmatic-devteams/dogfood/publish-hydra"
  (plus #263/#264/#266 publish merges today). Publish path healthy.
- Local checkout will fast-forward on the daemon's normal publish tick — not an
  operator action. The "51m unpublished" premise is stale.

## 3. Disk at 99% on /

- `df -h /` → 215G/451G = **50%**, far below the 85% watermark. The 99% claim is
  stale (recurring false alarm class, same as 0ndi/ew7t/8gvw/uzmx/cgud triages).
- No systemd-tmpfiles cleanup or cache capping required. diskCleanupGuard
  (a4d40f4) is deployed for real breaches.

## Disposition

Close as stale-snapshot; no mechanical action. Open PRs on the repo (PR #208–#267)
are normal dogfood bead PRs, not the referenced publish PR — do not admin-merge
anything for this bead.
