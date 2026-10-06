# Orphan-claim / DAG-wedge triage — hydra-uzmx (2026-10-06 12:53 UTC)

## Verdict
STALE ANOMALY — all three premises in the bead body verified false at triage time. No code change required; every upstream fix is landed and live.

## Evidence

### 1. "10 in_progress beads with no live registry worker" — FALSE
Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` at 12:53 UTC:

- Every in_progress bead has a registry row. Working rows (heartbeat <1 min): hydra-95ia, hydra-oj1v, hydra-m5j9, hydra-uzmx (this bead).
- Remaining in_progress beads carry `completed` rows with heartbeats 12:23–12:51 UTC — all heartbeat-fresh, counted live per the landed liveness rule (0d4aeaa): cw35, bldo, q207, rxi6, s8l7, lz3m, m3au, m72l, gcal, ew7t, cgud, pqq5, 8gvw.
- Live orphan claims = **0**.
- The digest "P0 campaign anomaly" line fired **0 times** in events/2026-10-06.jsonl.
- The non-working-row beads are human-gated, not orphaned: q207 + rxi6 carry `escalated/needs-human/verified-close-escalated` (excluded from OrphanClaims by 7548196); ew7t/cgud/pqq5/8gvw are `pan-filed` meta filings of this very anomaly (excluded by 7ef09f6).

### 2. "Orphan-claim reconciliation / Charon recovery not firing" — FALSE
Charon is actively recovering real orphans today (events/2026-10-06.jsonl, `charon_orphan_reopened` reason=dead_worker): hydra-nydy 12:18, hydra-lkbo 12:19, hydra-2a3f 12:21, hydra-s8l7 12:21, hydra-lz3m 12:21. Scheduler `reconcileOrphanClaim` (bb75c4a) + digest grace (0a2a6c9) + escalated/pan-filed exclusions (7548196/7ef09f6) all landed via PR #70 chain and are live.

### 3. "Disk 99% on /" — STALE
`df -h /` → 209G/451G = **49%**, below the 85% watermark. No systemd-tmpfiles cleanup or cache cap required; no I/O-error-28 risk. (Disk-cleanup guard i6we a4d40f4 is also landed and would fire automatically at breach.)

## Disposition of the wedge-filing twin beads
The 6 P0 campaign-anomaly pan-filed twins (hydra-m5j9, hydra-oj1v, hydra-8gvw, hydra-ew7t, hydra-cgud, hydra-pqq5) are duplicate filings of the same stale anomaly class and are being consumed by their own worker claims right now. They should be closed as duplicate-of hydra-uzmx (or of whichever twin completes first) once their workers exit — do not force-release claims on beads with live/fresh-heartbeat workers.

## Human review recommendation
No force-release warranted: no in_progress bead lacks a live (working or heartbeat-fresh) registry worker. The only beads a human might look at are the escalated pair q207/rxi6 (tdd-validation/close-gate escalations, human-gated by design).

## Prior landed fixes this triage confirmed live
- 7548196 — digest excludes human-escalated beads from OrphanClaims
- 7ef09f6 — digest excludes pan-filed meta beads
- 0a2a6c9 — 5m claim grace in digest
- 0d4aeaa — heartbeat-fresh completed rows count live
- bb75c4a — scheduler reconcileOrphanClaim (orphan_claim_reset)
- Charon recoverOrphanInProgressBeads incl. parked projects (vn9s fix)
- a4d40f4 — disk-cleanup guard (disk_cleanup_run at watermark breach)
