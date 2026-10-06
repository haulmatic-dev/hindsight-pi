# Triage: P0 orphaned in_progress claims wedging hydra DAG ladder (hydra-m5j9)

Date: 2026-10-06 ~12:5x UTC. Verified live; no code change (recovery path healthy).

## Verdict

Both P0 claims in the anomaly are stale or mischaracterized. The orphan-claim
reconciliation path is operating correctly on the current daemon; the recurring
"~9-10 orphaned in_progress claims" digest count is a transient churn
measurement, not a wedged ladder.

## Verified findings

### 1. Recovery IS live (reconciliation not broken)
- `charon_orphan_reopened` (dead_worker) ×6 today 12:18–12:21 UTC: hydra-sh9h,
  hydra-nydy, hydra-lkbo, hydra-2a3f, hydra-s8l7, hydra-lz3m — all reaped and
  reopened to open.
- 44 orphan events (`charon_orphan_reopened` + `orphan_claim_reset`) in
  2026-10-06.jsonl.
- Escalated orphans correctly routed with typed `orphan_escalated_wedged`
  events ("Charon will not reopen — resolution belongs to a human or Pan
  repair:clear-escalation"): hydra-q207, hydra-ee5r, hydra-k991, hydra-kvsc,
  hydra-rxi6. This is by-design behavior (human-gated beads are not
  machine-reopened).

### 2. Why the count persists (~9-10 "orphans" each sweep)
- 17 beads sit in_progress; only 3-4 have live working registry rows.
- Composition of the orphaned remainder:
  - 5 human-escalated beads (q207, rxi6, k991, kvsc, ee5r) — intentionally not
    reopened.
  - Gate-failed fix/validation-round beads (cw35, bldo, 95ia, m3au, m72l, gcal,
    lkbo, s8l7...) whose workers completed (registry rows state=completed) and
    which are between rejection-reopen and next spawn.
- The next spawn is throttled, not wedged: provider pool saturated
  (`[ProviderPool] No slot for bead hydra-uzmx (pool 10/10 (0 waiting)):
  provider pool: timed out after 2m0s waiting for 1 slot(s)`), so reopened
  beads queue for slots each tick. in_progress-without-worker is the normal
  state of a queued-then-reopened bead for minutes at a time.
- Each sweep snapshot counts these transients → Pan files a new P0 anomaly
  bead (hydra-fm8l filed 12:53:19) → filing cycle repeats. The prior
  pan-filed exclusion (7ef09f6) and escalated exclusion (7548196) are both
  landed (ancestor of origin/development verified) and deployed (binary mtime
  12:16:29 == daemon start 12:16:28).

### 3. Disk 99% claim STALE (no cleanup needed)
- `df -h /` → 209G/451G = **49%**, below the 85% watermark. No
  systemd-tmpfiles cleanup or cache cap required; no I/O-error-28 risk.

### 4. Host hygiene observed
- br WAL poison recurred ("Database error: database is busy (recovery in
  progress)"); `br doctor migrate-schema recover` cleared it (recurring
  known issue, prior triages 2vuc/8py6).

## Dispositions

- No scheduler/Charon code change: recovery path verified end-to-end on the
  deployed binary today.
- Escalated orphans (q207/rxi6/k991/kvsc/ee5r): human/Pan `clear-escalation`
  remains the lever; not a defect.
- Provider-pool saturation is the actual throttle on re-claiming reopened
  beads; if orphan-count noise continues to generate duplicate filings, the
  digest could add a retest-window grace for beads with a recent
  charon_orphan_reopened event — recorded as an idea, NOT implemented
  (smallest-change doctrine).
