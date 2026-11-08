# Orphan-claims recurrence triage — hydra-qqtl (2026-10-02)

## Snapshot at triage (03:0x UTC)

- hydra in_progress = 9: eight pan-filed P0 "campaign anomaly" twins
  (hydra-12di, hydra-s9sg, hydra-1euz, hydra-wv2e, hydra-yz8z, hydra-t2e1,
  hydra-qmf7, hydra-qqtl) + hydra-l867 (integration-exhausted hard-park,
  gate-failed — parked by design, not an orphan).
- Worker registry (`/opt/dev-tools/components/hydra/.hydra/registry.db`,
  `workers` table): 0 rows at inspection time. Pi worker processes were live
  (6+ hydra-owned). The registry is pruned between turns, so transient
  inprog>workers gaps are normal and Charon covers them — verified events:
  `charon_orphan_reopened` fired 3× today (00:52 fmsfe-sn5, 02:45 hydra-1p1q,
  02:47 hydra-vk94, all `reason: dead_worker`).
- Disk: `df -h /` = 162G/451G = **38%** — every "99% disk" claim in the twin
  beads is stale.

## Root cause of recurrence (confirmed, same as hxaj)

Self-referential loop:

1. Fleet digest reports `inprog` and `workers` per project.
2. Pan-filed meta beads (label `pan-filed`) sit in_progress forever — no
   worker ever claims them (they are human-review filings).
3. Pan (and the digest consumer) infers `orphanclaims = inprog − workers`.
   Pan-filed meta beads inflate inprog → phantom orphan claims every tick.
4. Each judgment files another pan-filed twin about the anomaly. Varying
   titles defeat file-bead dedupe. One day: 8 twins.

Real orphans (worker died mid-turn) are already handled: scheduler
`reconcileOrphanClaim` (1a3e643) + Charon orphan recovery (verified live
today).

## Fix landed (this bead)

internal/core/pan_health.go: pan-filed beads excluded from the digest
`InProgress` count — Pan-filed meta beads are filed for humans, never get
workers, and must not be counted as claimed work. Test:
`TestCollectHealthDigestPanFiledNotCountedInProgress`.

## Live-store disposition

Closed as duplicates (all carried the hxaj triage comment or were filed
mid-triage): hydra-12di, hydra-s9sg, hydra-1euz, hydra-yz8z, hydra-t2e1,
hydra-qmf7, hydra-wv2e, hydra-oqp8. Comments added on each.

Remaining in_progress: hydra-qqtl (this bead), hydra-l867 (parked work bead —
its integration-exhausted park is durable by design per prior triages; br
close blocked by gate-failed-build labels, human-only).

br WAL poison recurred during closes ("database is busy (recovery in
progress)"); `br doctor migrate-schema recover` cleared it.

## Open edges

- After this fix deploys, digest inprog for hydra = real worker-claimed beads
  only; phantom orphanclaims should stop recurring. If an orphanclaim count
  still appears with 0 pan-filed beads in_progress, it is a REAL orphan →
  identify bead ID and let Charon/scheduler reconciliation recover it.
- hydra-l867 remains the one genuinely wedged work bead (build-gate fail +
  VCS status error 128 in its worktree) — needs its own human triage, not
  part of the orphan anomaly.
