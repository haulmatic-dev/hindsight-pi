# task handoff (hydra-oqp8)

## What changed (files + one line each)
- docs/discovery/orphanclaim-disk99-triage-oqp8.md — triage: both P0 anomaly claims (orphaned in_progress claims, disk 99%) verified false at 2026-10-02 03:29 UTC; no code change — all upstream fixes already landed.
- CHANGELOG.md — Documentation entry under Unreleased.
- docs/hydra/handoff/task-oqp8.md — this handoff.

## Verified findings
- Disk: `df -h /` → 163G/451G = **38%** (watermark 85%) — the 99% digest claim is stale/false; no cleanup needed, no I/O-error-28 risk. PASS.
- Orphan claims: registry `$HYDRA_HOME/.hydra/registry.db` state=working → 7 rows, every heartbeat < 1 min old (incl. hydra-4jas anomaly bead and this bead hydra-oqp8) → orphanclaims = 0 live. The named beads (hydra-4jas, r07o, mw0j, ftah — all pan-filed) are actively claimed. No human reclaim needed. PASS.
- Charon recovery live-confirmed: 3 `charon_orphan_reopened` events today (fmsfe-sn5 00:52, hydra-1p1q 02:45, hydra-vk94 02:47). PASS.
- Upstream fixes all present on origin/development (tip 5efedd9, PR #70): `bb75c4a` scheduler reconcileOrphanClaim, `96087b0` park-must-not-block-recovery, `7ef09f6` digest excludes pan-filed meta beads, `0a2a6c9` digest 5m claim grace, `0d4aeaa` heartbeat-fresh completed rows count live. PASS (git grep origin/development).
- Prior triage `docs/discovery/orphanclaim-digest-grace-qmf7.md` (landed) already verified the false-alarm class; this bead's snapshot predates the fixes going live.

## Open edges for the next phase
- Residual pan-filed anomaly beads (4jas/r07o/mw0j/ftah) close through their own chains; do not close from here (gate race).
- If orphanclaims fires again post-PR#70 deploy, triage with registry heartbeat evidence before filing a fix bead — the counter's false-alarm modes are now enumerated and all patched.
