# Fleet anomaly triage: 19 orphaned in_progress claims + publisher wedge + disk 99% (hydra-p16t)

Date: 2026-10-06 ~14:1x UTC. All three P0 claims in the bead body verified STALE / self-resolved. No code change.

## 1. "19 orphaned in_progress claims wedging the DAG ladder" — FALSE

- Store `br list --status in_progress` (hydra store): **5** in_progress beads (bldo, q207, lb7g, e2a7, jdvp, 7tzf, dyw2, rxi6, 6t5v, p16t — 10 listed), not 19.
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: **6 live `working` rows** with heartbeats <30s (p16t=self, n9nq, xcye, twpj, 6t5v, 0tat). Every non-live in_progress bead (bldo, q207, lb7g, e2a7, jdvp, 7tzf, dyw2, rxi6, …) holds a `completed` registry row with a recent heartbeat (<1h) — the retry-gap churn snapshot class already triaged in hydra-hu12/qmf7/wwj2.
- Charon recovery LIVE: 48 `charon_orphan_reopened` events in events/2026-10-06.jsonl. Zero `P0 campaign anomaly` filings today.
- No live orphan claims; reconciler + Charon operating.

## 2. "Publisher wedge: locally-merged work unpublished 1h6m, PR awaiting human merge" — SELF-RESOLVED

- Local checkout `/opt/dev-tools/components/hydra` HEAD f6ec43c (`hydra: integrate hydra/wt-hydra-xw4q`) is now an **ancestor of origin/development**.
- PR #269 (dogfood/publish-hydra → development): `state: MERGED`, `mergedAt: 2026-10-06T14:14:44Z`, mergeCommit 5453080 = **current origin/development tip** (`git ls-remote origin development` → 5453080).
- Note: at fetch time GitHub reported `mergeStateStatus: UNKNOWN`; local merge-tree `--write-tree origin/development origin/dogfood/publish-hydra` was CLEAN (rc=0). The human merge landed minutes after the bead started.
- No human action outstanding; remote tip == PR #269 merge commit.
- Incidental: 9+ stale per-bead dogfood PRs (#252/253/257/258/262/265/267…) remain OPEN and CONFLICTING — publisher churn, not a wedge (same class as gzkr triage).

## 3. "Disk at 99% exceeds 85% watermark" — STALE

- `df -h /` → 167G/451G = **39%** (watermark 85%). No I/O-error-28 risk.
- `systemd-tmpfiles --clean` executed as hygiene (expected permission-denied noise on /root-owned dirs).
- diskCleanupGuard (a4d40f4) already deployed for real breaches.

## Incidental (recorded, not fixed)

- Main checkout git output shows `fatal: failed to run reflog` + `error: task 'gc' failed` during fetch — auto-gc failing, likely a stale reflog lock or permissions in the shared checkout. Does not block publish (merge landed). Candidate follow-up bead.

## Disposition

No code change; no live-store mutation required (no wedged orphans, no unmerged work, no disk breach). Scheduled recovery paths (Charon, reconcileOrphanClaim, integration_retest) verified operating.
