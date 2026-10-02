# Triage: orphaned in_progress claims wedging hydra DAG ladder + disk 99% (hydra-6d01)

Verified 2026-10-02 08:16–08:30 UTC.

## Claim 1: "4 in_progress beads with no live registry worker" — FALSE ALARM (escalated by design)

- The 4 beads: `hydra-bxzr`, `hydra-1h4v`, `hydra-wiu6`, `hydra-cxpf` — all GREEN-phase
  implementation beads that died as `tdd-validation-failed`, verified-close rejected 3/3,
  escalated to human with the bead left in_progress (`bead.escalated` events 07:20–08:02 UTC).
- Registry `$HYDRA_HOME/.hydra/registry.db`: 7 `working` rows, every heartbeat < 1 min old
  (incl. this bead and the sibling anomaly beads 65wl/fd5w/f4oa). Workers for bxzr/cxpf had
  `completed` normally with exit 0 before the escalation. Zero live orphans.
- Charon already reaped the class once today: `charon_orphan_reopened` (dead_worker) for
  bxzr 07:47, cxpf 07:50 — workers re-ran, re-failed tdd validation, re-escalated. Reaping
  is working; the beads are legitimately human-gated.
- Root cause of the recurring digest anomaly: the OrphanClaims counter counts any
  in_progress bead without a live worker row — escalated beads never get a worker, so the
  P0 anomaly re-fires for exactly the beads the escalation gate already owns. This is the
  same false-alarm family as hydra-fd5w/x60p/oqp8.
- Fix landed in this bead: pan_health.go excludes beads carrying the `escalated` label from
  OrphanClaims (test `TestCollectHealthDigestEscalatedBeadNotOrphan`). Aging is owned by
  escalation_ttl (`overseer.escalation_ttl`, 72h demote).

## Claim 2: "host disk 99% on /" — STALE

- `df -h /` → 183G/451G = **43%** (watermark 85%). No systemd-tmpfiles cleanup or cache
  capping needed; no I/O-error-28 risk. Note: `.hydra-worktrees` is 26G — the largest
  single consumer of runtime state, but far below any actionable threshold.

## Store health

- br WAL-index poison recurred at 08:16 (`database is busy (recovery in progress)`;
  one failed `private-recovery` attempt with pre-state retained). Cleared by
  `br doctor migrate-schema recover` (second attempt succeeded). Recurring pattern,
  already known (2vuc/8py6/bmtb triages).

## Disposition

- No orphan-claim reaper needed — Charon recovery + scheduler `reconcileOrphanClaim`
  + digest grace all live (PR #70 chain). The escalated-label digest exclusion is the
  only remaining false-alarm source and is fixed in this branch.
- The 4 escalated GREEN beads are the real open work: tdd-validation-failed ×2 rounds
  each — a human or Pan clear-escalation repair must decide (one fair retry vs
  abandon-with-summary).
