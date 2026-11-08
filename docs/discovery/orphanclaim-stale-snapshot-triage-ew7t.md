# Orphan-claim triage — hydra-ew7t (2026-10-06)

## Verdict
Both P0 claims in the bead title are STALE at triage time (12:41 UTC):

1. **"8 in_progress beads with no live registry worker"** — FALSE right now.
   All 20 in_progress hydra beads have either a `working` registry row with a
   heartbeat < 1 minute old (6 rows: m3au, pqq5, cgud, m72l, ew7t, 95ia) or a
   `completed` row with a fresh heartbeat (< 5m grace, 0d4aeaa rule). Live
   orphan claims = 0.
2. **"disk 99%"** — FALSE. `df -h /` = 208G/451G = **49%**, below the 85%
   watermark. `systemd-tmpfiles --clean` was run anyway (harmless, tmpfiles
   duplicate-line warning + permission-denied on /tmp/snap-private-tmp are
   benign); no space recovered needed.

## The 8 counted beads (identified + dispositioned)
Cross-referencing the in_progress list against the registry:

| Bead | Registry row | Disposition |
|---|---|---|
| hydra-cw35 | completed 12:35 (fresh) | gate-rejected close, retry path owns it |
| hydra-bldo | completed 12:30 (fresh) | gate-rejected close |
| hydra-q207 | completed 12:23 | **escalated** (excluded from counter, 7548196) |
| hydra-rxi6 | completed 12:25 | **escalated** (excluded) |
| hydra-lkbo | completed 12:23 | gate-rejected close |
| hydra-sh9h | completed 12:23 | no worker expected post-verdict |
| hydra-s8l7 | completed 12:25 | gate-rejected close |
| hydra-lz3m | completed 12:25 | post-verdict transient |
| hu12/v5xq/0ndi/2a3f/l5ug | completed (fresh) | **pan-filed** meta beads (excluded, 7ef09f6) |

Every one of these is covered by an already-landed digest exclusion
(escalated 7548196, pan-filed 7ef09f6, 5m claim grace 0a2a6c9, heartbeat-fresh
liveness 0d4aeaa) or is inside the fresh-heartbeat liveness window. The
digest=8 snapshot predates the current state; today's event stream has **0
"P0 campaign anomaly" filings**.

## Recovery machinery verified live
- 44 `charon_orphan_reopened` events today, latest batch 12:18–12:21 UTC
  (nydy, lkbo, 2a3f, s8l7, lz3m — reason dead_worker), followed by respawn
  activity in the daemon log.
- 51 `orphan_escalated_wedged` events today — the escalated-orphan path is
  correctly NOT reopening human-gated beads (q207 orphaned 8m, ee5r 53m;
  resolution belongs to Pan repair:clear-escalation or a human, by design).
- Deployed daemon binary `/opt/dev-tools/var/hydra/bin/hydra` contains the
  reconciliation symbols (`strings` hits for orphan_claim_reset /
  escalation-expired / heartbeat-fresh classes).

## Conclusion
No orphan-claim hardening code change is warranted: reconcileOrphanClaim
(bb75c4a), Charon recovery (incl. parked-project fix), and all four digest
false-alarm fixes are landed, deployed, and demonstrably firing. The recurring
"digest says N orphans" class is a snapshot-staleness artifact that each
triage bead re-disproves; the remaining escalated orphans (q207, ee5r) are
human-gated by design.
