# P0 orphaned in_progress claim + host disk 99% — triage (hydra-0ndi)

Date: 2026-10-06 ~12:30 UTC. Both P0 claims in the fleet digest are STALE / by-design. No code change (verify-first close; the recovery machinery is deployed and operating).

## Claim 1: orphaned in_progress claim (orphanclaims=1) wedging DAG ladder — FALSE

- Daemon PID 3172191 running `/opt/dev-tools/var/hydra/bin/hydra -config /etc/hydra/hydra.yaml`; binary contains `orphan_escalated_wedged` + `reconcileOrphanClaim` symbols (strings hits = 10) → escalated-exclusion (PR #144, 7548196) + scheduler orphan reconcile (bb75c4a) + Charon recovery are DEPLOYED.
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: every in_progress bead with a live worker has a fresh (<30s) heartbeat. `hydra-0ndi` (this bead) itself is in the registry as `working`.
- No `P0 campaign anomaly` filing in events 2026-10-03..2026-10-06.
- Charon actively recovering real orphans today: `charon_orphan_reopened` hydra-nydy 12:18:31, hydra-lkbo 12:19:30, hydra-2a3f 12:21:31, hydra-s8l7 12:21:31, hydra-lz3m 12:21:32 (reason dead_worker). Recovery loop is live and working.
- In_progress beads with NO registry row: hydra-k991, hydra-kvsc, hydra-ee5r — all carry `escalated` + `needs-human` labels (verified-close-escalated human gates). No worker is EXPECTED; digest excludes escalated beads by design (7548196). Charon correctly refuses to reopen them and emits typed `orphan_escalated_wedged` events pointing at human/Pan `repair:clear-escalation`.
- Any DAG wedging by these escalated beads is the designed human-gate behavior, not an orphan-claim defect. Scheduler reconciliation is NOT failing.

## Claim 2: host disk 99% (watermark 85%) — FALSE

- `df -h /` → 208G/451G = **48%** (watermark 85%). No I/O-error-28 risk; br not failing.
- `systemd-tmpfiles --clean` run as hygiene; no material change (not needed). Disk-watermark cleanup guard (a4d40f4) already deployed in daemon.

## Conclusion

Recurring incident class (6th+ time: 12di, qmf7, x60p, yusp, 5fbq, kxso, pf9c, 6d01, oqp8): digest snapshot claims of orphanclaims + disk-99% are stale by the time a bead investigates. All upstream fixes (claim grace, pan-filed exclusion, heartbeat-fresh liveness, escalated exclusion, Charon park-recovery, disk cleanup guard) are landed and deployed. Nothing to reconcile mechanically; no hydra code change warranted — reconciliation does not repeatedly fail, there is no live orphan to reconcile.

Human-facing note: k991/kvsc/ee5r are awaiting human/Pan review (escalated). If the ladder they block is urgent, Pan `repair:clear-escalation` gives one fair retry.
