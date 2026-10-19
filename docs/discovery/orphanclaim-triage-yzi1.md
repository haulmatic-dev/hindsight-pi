# Orphan-claims / publisher-wedge / disk-99% triage — hydra-yzi1

Triage at 2026-10-06 14:00–14:06 UTC. All three P0 claims in the digest are stale or
false; recovery loops verified live; no code change required. Bead left open for
human review per anomaly-bead policy.

## Claim 1: 17 orphaned in_progress claims — FALSE (0 true orphans)

Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`:
- 4 live `working` rows (hydra-n9nq, hydra-xcye, hydra-qas9, hydra-yzi1), heartbeats
  < 60 s old, PIDs alive. PASS.
- `hydra-7tzf` / `hydra-dyw2` have completed rows with empty `completed_at`
  (in-flight close bookkeeping); both beads re-claimed and progressing.

Cohort disposition of the 17 counted in_progress beads:
- 2 escalated/needs-human (hydra-rxi6, hydra-q207) — excluded from the digest by
  design (7548196); awaiting human/Pan review, not orphans.
- ~9 pan-filed campaign meta beads (qas9, qv28, fg7e, xcye, yc0m, 4p4d, 3jxo, faeu,
  6dbv, rz63) — excluded per 7ef09f6; these are the anomaly's own filings.
- Remaining gate-failed/discovery beads (lb7g, e2a7, jdvp, gcal, m72l, xmla, n5rv,
  mt7e, 7w2f, uppd, ngui, 96th, bldo) — every one has a registry `completed` row
  exited 13:18–13:58 UTC (exit 0), i.e. retry-gap churn inside the respawn
  backoff window, actively re-opened by Charon.

Charon recovery LIVE: 48 `charon_orphan_reopened` events in
`/opt/dev-tools/var/hydra/events/2026-10-06.jsonl` (top: hydra-bldo ×6, hydra-ewe3 ×5).
Digest filed **0** "P0 campaign anomaly" events today. The 17-count snapshot came
from a single digest tick before the churn cohort's next respawn; nothing wedged.

## Claim 2: publisher wedge / PR awaiting human merge — ABSENT

- `git ls-remote origin development` → 53eca1b = `Merge pull request #268 from
  haulmatic-devteams/dogfood/publish-hydra` — the PR merged itself via the
  established admin-merge path (precedent PRs #85/#148/#162/#178).
- `gh pr list --head dogfood/publish-hydra --state open` → empty. No PR awaiting a
  human.
- Local checkout `/opt/dev-tools/components/hydra` HEAD 992971f:
  `git rev-list --count origin/development..HEAD` = 0 — zero unpublished commits.
  The checkout is behind origin by 2 merge commits (daemon fast-forward territory).
- Observed (not acted on): the checkout working tree carries in-flight staged
  deletions (`git diff HEAD --stat` = 16 files, −395) consistent with a concurrent
  daemon integrate operation mid-flight at inspection time. Not touched.

## Claim 3: host disk 99% — FALSE

`df -h /` → 165G used / 451G = **39%**, far below the 85% watermark. No
ENOSPC/I-O-error-28 risk. `diskCleanupGuard` (a4d40f4) is deployed and would have
run on a real breach.

## Deployed binary

`/opt/dev-tools/var/hydra/bin/hydra` mtime 2026-10-06 12:16:29 == daemon start
(PID 3172191, started 12:16:28). Digest exclusions (escalated 7548196, pan-filed
7ef09f6, grace 0a2a6c9) and reconcileOrphanClaim all present on origin/development
and in the running binary lineage — no stale-binary gap this round.

## Disposition

- No human merge action needed (claim 2 self-resolved via PR #268).
- No orphan reconciliation action needed (Charon + scheduler reconcile live, 0 true
  orphans, 0 anomaly filings today).
- No disk cleanup needed (39%).
- If the digest re-fires with the same numbers: re-check deployed binary mtime vs
  origin/development tip first (recurring stale-binary trap), then verify the
  churn cohort's registry rows are heartbeat-fresh/completed within grace before
  treating any bead as a true orphan.
