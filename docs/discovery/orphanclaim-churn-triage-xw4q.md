# Triage: orphanclaims=14 P0 anomaly + disk 99% — hydra-xw4q (2026-10-06 13:1x UTC)

## Verdict: BOTH CLAIMS STALE / BY-DESIGN. No code change. Reconciler IS firing.

## Orphan claims

Snapshot at bead start (`br list --status in_progress`, hydra store, after
`br doctor migrate-schema recover` for recurring WAL poison
"database is busy (recovery in progress)") — 25 in_progress beads:

- **9 live workers** (`working`, heartbeats < 60s): lb7g, e2a7, jdvp, 7tzf,
  dyw2, xw4q (this bead), 7w2f, n5rv, fm8l. Not orphans.
- **9 pan-filed meta beads** — excluded from OrphanClaims by design
  (7ef09f6): yt1y, uzmx, m5j9, oj1v, 8gvw, ew7t, cgud, pqq5 (+xw4q/fm8l
  themselves live).
- **2 escalated human-gated** — excluded by design (7548196); Charon emits
  `orphan_escalated_wedged` every 15m as expected: q207, rxi6.
- **6 churn candidates** with completed registry rows (workers exited
  normally, exit timestamps 12:30–12:57): cw35, bldo, 95ia, m3au, m72l,
  gcal. These are the pre-grace snapshot window the digest counts — same
  class as hydra-hu12 (12:40 triage) and the landed 5m grace chain
  (0a2a6c9/0d4aeaa).

Recovery is LIVE, not stuck:
- `charon_orphan_reopened (dead_worker)` events today: 95ia 12:13, sh9h
  12:18, nydy 12:18, lkbo 12:19, 2a3f 12:21, s8l7 12:21, lz3m 12:21 +
  13:06. Daemon restarted 12:16 UTC (PID 3172191) with the current binary;
  `reconcileOrphanClaim`/`orphan_claim_reset`/`orphan_escalated_wedged`
  symbols verified in deployed binary (prior 0ndi handoff).
- Fresh `gate_run` events at 12:51–12:52 on cw35/bldo/95ia/m3au/m72l/gcal —
  the churn cohort is actively being retried, not wedged.
- Zero `P0 campaign anomaly` filings in events today; the 14-count digest
  was a pre-restart churn snapshot.

The churn cohort (cw35/bldo/95ia/m3au/m72l/gcal) carries
`gate-failed`/`verified-close-reject-*` labels — gate-failure churn loop,
self-terminating via the durable escalation ladder landed by 95ia
(12:57). Not a reconciliation defect.

## Disk

`df -h /` → 210G/451G = **49%** (watermark 85%). The 99% claim is stale.
`systemd-tmpfiles --clean` run anyway as hygiene (tmp.conf duplicate line
warning + /tmp permission-denied entries from other-user dirs, expected).
Largest consumer is `/opt/dev-tools/var/hydra` at 62G (worktrees + events,
normal runtime state). No I/O-error-28 risk.

## Human review items

- q207, rxi6: escalated, awaiting human judgment (Pan
  `repair:clear-escalation` = one fair retry, or abandon-with-summary).
- Churn cohort burns one worker per retry round; if the gate-failure loop
  persists past the escalation ladder's cap, next triage should check the
  ladder actually escalates them (labels `verified-close-reject-attempt-N`).

## Verification (commands run)

- `df -h /` → 49%. PASS.
- `br list --status in_progress --json` + registry.db cross-check → 9 live
  workers, 0 live orphans. PASS.
- events 2026-10-06.jsonl → Charon reopen loop live, zero P0 anomaly
  filings. PASS.
- `systemd-tmpfiles --clean` → executed. PASS.
- hindsight_search at bead start — not connected (skip rule applied).
- graphify-out/graph.json absent in cwd → rg + live-store fallback per
  protocol.
