# Triage: 14 orphaned in_progress claims + host disk 99% (hydra-bkip)

Verified live at 2026-10-06 13:16–13:22 UTC. **Both P0 claims stale or by-design; no code change. Recovery is self-healing for this cohort.**

## Disk: 99% claim STALE

`df -h /` → 210G/451G = **49%** (watermark 85%). No systemd-tmpfiles cleanup or cache cap required; no I/O-error-28 risk. The `diskCleanupGuard` (a4d40f4) is deployed and would fire if the watermark were actually breached.

## Orphan-claim cohort: full cross-check

Live in_progress beads in the hydra store (25 at check time) crossed against
`$HYDRA_HOME/.hydra/registry.db` worker rows. 17 had no live `working` row.
Breakdown:

### 1. Pan-filed pseudo-beads (8) — excluded from digest by design (7ef09f6)

hydra-yt1y, hydra-uzmx, hydra-m5j9, hydra-oj1v, hydra-8gvw, hydra-ew7t,
hydra-cgud, hydra-pqq5 — all `pan-filed`, all with dead `completed` registry
rows. These are duplicate filings of this same anomaly class, not work beads.

### 2. Escalated to human (5) — Charon skip by design (`orphan_escalated_wedged`)

- **hydra-rxi6**, **hydra-q207** — escalated 2026-10-06 ~12:2x (verified-close
  rejected, needs-human). Fresh; human queue.
- **hydra-k991**, **hydra-kvsc**, **hydra-ee5r** — escalated since 2026-10-04
  (verified-close-escalated). Under the 72h escalation TTL; the TTL scanner
  will demote them automatically if no human acts first.

Charon refuses reopen (fail-closed by design), emits `orphan_escalated_wedged`
with 15m cooldown per bead — verified in events.

### 3. Close-gate serialized backlog (3) — Charon correctly leaving in_progress

**hydra-m3au**, **hydra-m72l**, **hydra-gcal** — worker exited, close hard-gate
cycle running. Gate artifacts at `/opt/dev-tools/var/hydra/gates/hydra/` show
`running` since 12:52 with the serialized per-project gate lock queueing them
behind sibling gates (bldo's lint gate alone ran 12:30→12:51). Charon logs
`close in progress (gate artifact running); leaving in_progress` every sweep —
correct: the close is genuinely mid-flight and the 45m artifact age cap bounds
a stuck artifact.

### 4. Gate-failure reopen churn (3) — the real churn, and it IS being recovered

**hydra-cw35** (2 reopens), **hydra-bldo** (6 reopens), **hydra-95ia**
(3 reopens) — `charon_orphan_reopened` (dead_worker) events today. Loop shape:
worker completes → close hard-gate runs (serialized) → gate fails
(cw35: real test-suite `FAIL`; 95ia: test gate exit **-1 = timeout** after 9m
with all logged packages `ok` — host CPU contention from concurrent full-suite
runs) → close rejected → bead reopens → worker respawns → repeat.

Charon recovery is **live and working** for exactly this cohort — the beads are
not stuck, they are churning. hydra-cw35's own title describes this loop
("integrate-failure reopen loop burns workers"); it is filed as its own task
bead and needs the park-as-integration-pending change, not orphan-claim work.

## Conclusions

1. **No wedged orphan exists.** Every in_progress bead without a live worker is
   accounted for: pan-filed exclusion, human escalation, in-flight close gate,
   or actively churn-recovered by Charon.
2. **No orphan-claim recovery code change is warranted.** Reconciliation
   (`reconcileOrphanClaim`), Charon orphan sweep, digest grace/exclusions are
   all deployed and functioning (multiple `charon_orphan_reopened` events today).
3. **Real cost driver** (secondary, already filed as hydra-cw35): close-gate
   failures on already-good work burn a full worker redo per cycle. The gate
   timeout class (95ia exit -1 with all packages ok) is contention-driven.
4. **Disk 99%: stale.** 49%, watermark 85%, cleanup guard deployed.

## Open edges for the next phase

- hydra-cw35 / hydra-bldo / hydra-95ia churn: wait on the filed task bead
  (park-on-integrate-failure) rather than patching recovery.
- Escalated queue k991/kvsc/ee5r: Pan `repair:clear-escalation` (one fair
  retry) or escalation TTL demotion at 72h.
- Gate-run serialization makes close verification 10–20 min per bead under
  fleet load; if the backlog grows, revisit gate timeout under contention
  (95ia timeout-kill with all-ok output is the wrong verdict for that state).
