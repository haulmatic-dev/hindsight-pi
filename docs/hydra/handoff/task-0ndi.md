# discovery handoff (hydra-0ndi)

## What changed (files + one line each)
- docs/discovery/orphanclaim-disk99-triage-0ndi.md: triage — both P0 claims verified stale/by-design; no code change.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verified findings
- Daemon PID 3172191 running `/opt/dev-tools/var/hydra/bin/hydra -config /etc/hydra/hydra.yaml`; binary strings contain `orphan_escalated_wedged` + `reconcileOrphanClaim` (10 hits) → scheduler reconcile + Charon + escalated exclusion DEPLOYED. PASS.
- Events 2026-10-03..2026-10-06: zero `P0 campaign anomaly` filings. PASS.
- Charon live today: `charon_orphan_reopened` ×5 (nydy 12:18, lkbo 12:19, 2a3f 12:21, s8l7 12:21, lz3m 12:21, dead_worker). Recovery loop operating. PASS.
- In_progress beads with no registry row: hydra-k991, hydra-kvsc, hydra-ee5r — all `escalated` + `needs-human` (verified-close-escalated). No worker expected; digest excludes escalated by design (7548196). Charon correctly refuses reopen, emits `orphan_escalated_wedged`. No mechanical reconciliation needed or failing. PASS.
- Registry: all working beads (incl. this bead hydra-0ndi) have fresh (<30s) heartbeats. 0 live orphans. PASS.
- Disk: `df -h /` → 208G/451G = **48%** (watermark 85%). `systemd-tmpfiles --clean` run as hygiene. No I/O-error-28 risk. PASS.

## Open edges for the next phase
- k991/kvsc/ee5r await human/Pan review; Pan `repair:clear-escalation` = one fair retry if their ladder is urgent.
- Recurring digest-false-alarm class (9+ prior triages) — if it keeps consuming beads, the durable fix is a snapshot-freshness guard in the digest consumer, not more orphan recovery.
