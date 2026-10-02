# Orphan-Claim + Disk-99% Triage (hydra-oqp8)

**Verdict: VERIFIED RESOLVED — both anomalies cleared upstream and live-verified; no code change.**

## Snapshot at triage (2026-10-02 03:29 UTC)

- `df -h /` → **163G/451G = 38%** (watermark 85%). The digest's `disk=99%` claim is stale/false; no systemd-tmpfiles cleanup or cache cap needed. I/O-error-28 risk absent.
- Registry (`$HYDRA_HOME/.hydra/registry.db`, state=working): 7 rows, every one with a heartbeat < 1 min old — including this bead (hydra-oqp8) and the pan-filed anomaly beads (hydra-4jas). **orphanclaims = 0 live**.
- Charon orphan recovery confirmed working live today: 3 `charon_orphan_reopened` events (fmsfe-sn5 00:52, hydra-1p1q 02:45, hydra-vk94 02:47).

## Why the anomalies were reported

Fleet digest counts in_progress beads with no live registry row as `orphanclaims` and 99% disk as host anomaly. Chain of false-positive modes, all fixed on origin/development (2026-10-01→02):

| Root cause | Fix (landed) |
|---|---|
| Retry-exhausted in_progress claim wedges DAG forever | `bb75c4a` scheduler `reconcileOrphanClaim` → reset to open + `orphan_claim_reset` event |
| Park blocked Charon orphan recovery | `96087b0` recovery runs for parked projects |
| Pan-filed meta beads counted as inprog/orphans → self-referential filing loop | `7ef09f6` digest excludes pan-filed meta beads |
| Spawn wave: claimed-but-no-registry-yet counted orphaned | `0a2a6c9` digest applies Charon 5m claim grace |
| Heartbeat-fresh completed rows counted as non-live | `0d4aeaa` (PR #70, merged 5efedd9) |

Prior triage `docs/discovery/orphanclaim-digest-grace-qmf7.md` already verified the false-alarm class; this bead's snapshot (orphanclaims=2, disk=99%) was from an older digest cycle before those fixes went live.

## The wedged in_progress beads named

The beads the digest named (hydra-4jas, hydra-r07o, hydra-mw0j, hydra-ftah — all `pan-filed` P0 anomaly filings) are claimed by live, heartbeat-fresh workers right now. **No claim needs human reclaim.** The self-referential churn is the mechanism; the upstream digest fixes stop it.

## Disposition

- Human reclaim of orphaned claims: **not needed** (none exist at triage time).
- Disk cleanup: **not needed** (38%, below watermark).
- Code change: **not needed** — all four upstream fixes merged to origin/development (tip 5efedd9).
- Residual anomaly beads in_progress (4jas/r07o/mw0j/ftah) will close through their own chains; closing them here would race their gates.

## Verification

- `df -h /` → 38% (pass).
- Registry heartbeat scan → 7/7 working rows fresh (pass).
- `git grep orphanclaims origin/development` → all digest fixes present (pass).
- Event scan 2026-10-02.jsonl → 3 charon_orphan_reopened, no orphan_claim_reset needed after fixes live.
