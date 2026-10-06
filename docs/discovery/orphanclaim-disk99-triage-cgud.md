# Triage: P0 orphaned in_progress claims + host disk 99% (hydra-cgud)

2026-10-06 ~12:40 UTC. Both P0 claims in the bead body verified stale; no code change.

## Claim 1: 6 orphaned in_progress claims wedging the DAG ladder

- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` (`workers` table): 7 rows in
  `working` state, every `last_heartbeat` < 1 min old (incl. hydra-cgud itself). No
  in_progress bead lacks a live or heartbeat-fresh row.
- Charon recovery is live and firing: multiple `charon_orphan_reopened` (reason
  `dead_worker`) events on 2026-10-06 for hydra-lz3m, hydra-cw35, hydra-bldo,
  hydra-95ia, hydra-sh9h, hydra-nydy, hydra-lkbo, hydra-2a3f, hydra-s8l7 — the
  orphan cohort named by the digest filings (incl. the "5 orphans nydy/lkbo/2a3f/
  s8l7/lz3m" snapshot) was already recovered during this bead's window.
- The remaining in_progress beads without a `working` row are human-gated by
  design: hydra-q207 and hydra-rxi6 carry `escalated`/`needs-human` (excluded from
  OrphanClaims by commit 7548196).
- `reconcileOrphanClaim` (bb75c4a), digest grace chain (5m claim grace 0a2a6c9,
  pan-filed exclusion 7ef09f6, heartbeat-fresh liveness 0d4aeaa) all landed via
  PR #70 and present on origin/development. reconcileOrphanClaim + Charon sweeps
  are the mechanical remedy named in the proposal — already deployed and working.
- The recurring filings themselves (0ndi/2a3f/ew7t/hu12/pqq5/v5xq pan-filed meta
  beads) are the digest→Pan twin-filing loop documented in prior triages
  (qqtl, oqp8, qmf7, 6d01); the fix chain is landed, filings are historical churn.

## Claim 2: host / at 99% disk

- `df -h /` → 208G/451G = **49%**, well below the 85% watermark. ENOSPC (I/O 28)
  risk not active; no systemd-tmpfiles cleanup or cache cap needed.
- Hydra-side disk guard already landed: `diskCleanupGuard`
  (internal/core/disk_cleanup.go, a4d40f4 via PR #147) runs `systemd-tmpfiles
  --clean` once per hourly cooldown when the digest crosses the watermark;
  `collectDiskWatermark` (85%, "/") feeds the digest. A scheduler-blocking
  preflight would be fail-closed against the whole pipeline — the fail-open
  cleanup + digest anomaly route is the deliberate design (see i6we handoff).

## Disposition

Verify-first close. No code change; no live-store mutation needed (orphans
recovered by Charon mid-triage; beads claiming to be wedged are worker-claimed
and fresh). File no follow-up: the "persisting anomaly" condition did not
persist past the sweep window.
