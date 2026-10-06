# Triage: orphaned in_progress claims + publisher PR unmerged + disk 99% (hydra-qas9)

Verified live 2026-10-06 13:59–14:05 UTC. All three P0 claims in the digest are
stale/false. No code change required; no operator action beyond routine churn.

## Claim 1: "18 in_progress beads, no live registry workers"

- `br list --status in_progress` (hydra store) — **0** beads in_progress.
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` — 5 `working` rows, all
  heartbeats < 15 s old (xcye, n9nq, 21xp, 0uzn, qas9=self); ~20 `completed`
  rows within the last 20 min (steady completion flow).
- Events `2026-10-06.jsonl`: 48 `charon_orphan_reopened` (dead_worker) — orphan
  recovery loop live and actively reaping/reopening; zero `P0 campaign anomaly`
  filings today (grep count 0).
- Digest counted an earlier snapshot (worker exit → respawn-backoff window),
  the exact false-alarm class already fixed by the 5m claim grace (0a2a6c9),
  pan-filed exclusion (7ef09f6), heartbeat-fresh liveness (0d4aeaa), and
  escalated exclusion (7548196) — all landed and deployed (binary mtime
  12:16:29 == daemon start, PID 3172191).

## Claim 2: "publisher PR unmerged; locally-merged work unpublished 1h+"

- Local checkout HEAD `992971f` is an **ancestor** of `origin/development`
  (`git merge-base --is-ancestor` YES); 0 unlanded local commits.
- `git ls-remote origin development` = `53eca1b` = local origin/development ref
  (fetch succeeded).
- Open PRs on haulmatic-devteams/hydra: 30, all `dogfood/hydra-<id>` feature
  PRs — **no** `dogfood/publish-hydra` PR open.
- Publish pipeline demonstrably healthy: 5 PRs merged in the last hour
  (#268 13:51:34Z, #266 13:35:22Z, #264 13:34:50Z, #263 13:25:30Z,
  #260 13:03:16Z).

## Claim 3: "host disk 99% on /"

- `df -h /` → 165G/451G = **39%**, far below the 85% watermark.
- No ENOSPC risk; no systemd-tmpfiles cleanup or cache cap needed.
- diskCleanupGuard (a4d40f4) remains deployed for real breaches.

## Disposition

No hydra code change: the anomaly is NOT persisting across sweep ticks (0 P0
filings today; in_progress count = 0), so the "propose claim-recovery hardening"
trigger condition is not met. The existing recovery chain — Charon
`recoverOrphanInProgressBeads`, scheduler `reconcileOrphanClaim`, digest grace
window + label exclusions — is deployed and observed working (48 reopens today).

The recurring P0 twin filings of this exact digest shape (0ndi, 2a3f, ew7t,
hu12, pqq5, v5xq, cgud, uzmx, 8gvw, this bead) are all verify-first closes; if
another fires, check deployed-binary mtime vs origin/development tip first
(stale-binary was the one real recurrence, fixed by drain-restart 5fbq).
