# Triage: P0 campaign anomaly — 15 orphaned in_progress claims + publisher wedge (hydra-xcye)

Date: 2026-10-06 ~14:00 UTC. Bead: hydra-xcye (pan-filed, source hydra-campaign-anomaly).

## Finding 1 — 15 orphaned in_progress claims: REAL DEFECT (digest), ladder NOT wedged

Snapshot at 13:53 UTC: 25 in_progress hydra beads. Exclusions per deployed
digest semantics: 7 pan-filed (incl. this bead), 2 escalated (q207, rxi6 —
`orphan_escalated_wedged` correctly firing, human/Pan owns them). Remaining 16
candidates: bldo, lb7g, e2a7, jdvp, 7tzf, dyw2, gcal, m72l, n9nq, mt7e, 7w2f,
uppd, ngui, 96th, xmla, n5rv.

Registry cross-check (`$HYDRA_HOME/.hydra/registry.db`): **every one of the 16
has a worker row, all `state=completed`** (heartbeats 13:18–13:41). At digest
sweep time (~13:41) the 15 counted were exactly the beads whose completed rows
had aged past the 5m heartbeat-fresh liveness window. These are the gate-reject
churn beads (bldo/gcal/lb7g `gate-failed-test`) and red/discovery chain beads
between worker attempts — the scheduler's respawn backoff owns their requeue,
and Charon was actively cycling them (48 `charon_orphan_reopened` today;
dyw2 13:17, gcal 13:27, m72l 13:34).

Root cause: the digest counts a bead orphaned once its only worker row is
`completed` with a stale (>5m) heartbeat, even though a completed row is
terminal worker state — the worker exited through the normal completion path
and cannot still be working. Pan then filed a campaign-anomaly twin every
sweep (3jxo 13:41, 4p4d 13:43, yc0m 13:45, xcye 13:49, fg7e 13:53), each
filing itself an in_progress claim feeding the next round. Same false-alarm
class as the 2026-10-02 grace loop (hydra-qmf7) and the heartbeat-fresh fix
(hydra-wv2e), one window later in the worker lifecycle.

Fix (this bead): `internal/core/pan_health.go` — a bead whose registry row is
`state=completed` is never counted as an orphaned claim regardless of heartbeat
age (new `completedByBead` map built from the same `ListWorkers` scan). Genuine
orphans — bead in_progress past the 5m claim grace with no row, or a row still
`working`/`hung` with a stale heartbeat — still count. Regression test
`TestCollectHealthDigestCompletedRowNotOrphan`.

## Finding 2 — publisher wedge: STALE / ABSENT

Local checkout `/opt/dev-tools/components/hydra` on `development` at 992971f,
`git rev-list --count origin/development..HEAD` = 0, remote tip 53eca1b =
"Merge pull request #268 from haulmatic-devteams/dogfood/publish-hydra". No
publish PR awaiting merge (`gh pr list` shows only per-bead dogfood PRs #257,
#258, #262, #265, #267 — worker strands, handled by the normal close path).
The "54m unpublished" premise predates PR #268's merge.

## Finding 3 — disk 99%: STALE

`df -h /` = 50% at bead start (215G/451G), 39% after `systemd-tmpfiles
--clean` (168G/451G). Watermark 85% not breached; no I/O-error-28 risk.
diskCleanupGuard (a4d40f4) deployed for real breaches.

## Dispositions

- Digest defect: fixed in `internal/core/pan_health.go` + regression test.
- Publisher wedge: no action — work already published via PR #268.
- Disk: hygiene cleanup run; no further action.
- q207/rxi6 escalated beads: human/Pan `clear-escalation` still pending
  (pre-existing, by-design exclusion, not part of this anomaly).
