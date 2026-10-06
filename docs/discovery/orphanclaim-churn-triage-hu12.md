# Orphan-claim P0 triage — hydra-hu12 (2026-10-06)

## Claim
Fleet digest persistently reports 6 in_progress beads with no live registry worker
(orphanclaims=6); scheduler orphan-claim reconciliation and Charon recovery failing
to reclaim; disk 99%.

## Verdict
Both claims FALSE as stated. Recovery is not broken — the counted beads are a
transient retry-gap snapshot of a gate-failure churn loop that self-terminates via
escalation.

## Evidence (all run live 2026-10-06 12:31–12:38 UTC)

### Store census (`br list --status in_progress`, hydra store) — 19 beads
| class | beads | digest treatment |
|---|---|---|
| escalated (human-gated, no worker by design) | k991, kvsc, q207, rxi6, ee5r | excluded (7548196) |
| pan-filed P0 meta beads | hu12, v5xq, 0ndi, l5ug, 2a3f | excluded (7ef09f6) |
| gate-fail churn / retry-gap | cw35, bldo, 95ia, sh9h, nydy, lkbo, s8l7, lz3m, gcal | the counted "orphans" |

### Charon recovery LIVE (44 `charon_orphan_reopened` events on 2026-10-06)
Reopen counts per bead today: bldo 6×, sh9h 4×, lkbo 4×, 95ia 3×, nydy 3×, s8l7 3×,
lz3m 3×, cw35 2×. Every named candidate was reopened and respawned.

### Registry state (12:36 UTC)
8 of 9 candidate beads have `completed` registry rows (workers exited normally after
gate-test failure); 95ia + hu12 + v5xq live `working` with fresh heartbeats.
Progress is NOT silent zero — 3 workers actively running.

### Why the digest counts them
`pan_health.go` grace key is bead `UpdatedAt` (claimsByID). Between a worker's normal
exit (gate reject → bead stays in_progress) and the scheduler's next respawn (retry
backoff > 5m), the bead is in_progress with no live registry row → counted as an
orphan claim. Charon reopens any that actually stall; the scheduler re-spawns the
rest. The count is a snapshot of that gap, not a stuck state.

### Convergence proof
The churn loop terminates: 5 beads already reached `escalated` (verified-close
rejected at max attempts) and are human-gated — the escalation_ttl scanner owns
their aging. Each remaining churn bead burns one retry per pass and will escalate
the same way.

### Disk
`df -h /` → 208G/451G = 49%, below the 85% watermark. 99% claim stale. No
systemd-tmpfiles cleanup or cache cap required; br WAL poison (recovered once via
`br doctor migrate-schema recover` at 12:35Z) is the known transient, not disk.

## Disposition
No code change. No manual release/requeue needed — Charon + scheduler already own
every candidate bead. The repeated gate-test failures (work content failing
`go test` on retry) are the real cost driver; if the churn needs a knob, that is a
separate bead (per-bead retry budget before escalation already exists via
verified-close attempt cap).

## Residual risk
The digest will keep surfacing 4–8 "orphans" while a gate-fail churn cohort is in
flight. Options for a future bead: key the digest grace on the registry row's
completed-at time (worker exit) rather than bead UpdatedAt, or exclude beads whose
latest registry row is a terminal-state recent completion.
