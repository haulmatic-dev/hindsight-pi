# Orphan-claims P0 triage (hydra-6dbv) — 2026-10-06 ~13:45 UTC

## Claims audited

| Claim | Verdict | Evidence |
|---|---|---|
| 13 orphaned in_progress claims, 0 live workers | **STALE** | Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 4 `working` rows (hydra-n9nq, rhin, cbww, 6dbv) all heartbeats <1 min old; every other in_progress bead has a `completed` row with heartbeat-fresh liveness (<5m grace, 0d4aeaa) except hydra-95ia (below). 0 live orphans under digest semantics. |
| Orphan reconciliation + Charon not firing | **FALSE** | events/2026-10-06.jsonl: 48 `charon_orphan_reopened` (dead_worker) today, latest batch 12:18–12:21 UTC; 58 `orphan_escalated_wedged` (escalated beads q207/rxi6 correctly left for human). `reconcileOrphanClaim` fires only for retry-exhausted orphans — 0 today means no such case, not a failure. Deployed binary built 12:16 from origin/development tip (all exclusion fixes live). |
| P0 campaign anomaly persisting | **STALE** | 0 "P0 campaign anomaly" filings in today's event log. Digest quiet. |
| Publisher wedge: locally-merged work unpublished 39m, PR awaiting human merge | **STALE** | Local checkout `/opt/dev-tools/components/hydra` HEAD 9993250 (development): 0 commits ahead of origin/development, 6 behind; no open `publish`-head PR on haulmatic-devteams/hydra (10 open dogfood/* PRs are per-bead strands awaiting their own integration, not a publish wedge). |
| Host disk 99%, I/O-error-28 risk | **STALE** | `df -h /` → 213G/451G = **50%** (watermark 85%). `systemd-tmpfiles --clean` run as hygiene at user scope (`systemd-tmpfiles --user --clean`, rc=0; root-level clean requires tty password, unnecessary at 50%). |

## Wedged beads named for human review

- **hydra-95ia** — the only in_progress bead without fresh liveness (registry `completed` row 12:57 UTC, 43m stale). Charon reopened it 3× today (11:44, 12:05, 12:13 dead_worker) — a churn loop: prior closes rejected `verified-close-reject-gate-runner-fault-lint`/`-test-failed`, current gate runs PASS (lint 13:34, build 13:35 UTC). Triage comment added. Human/Pan: one fair scheduler retry now that gates pass, or abandon-with-summary if the loop recurs.
- **hydra-q207, hydra-rxi6** — `escalated`/`needs-human` human-gated, excluded from the digest by design (7548196); need human review, not reconciliation.
- **hydra-wwj2, hydra-gzkr** — pan-filed duplicate twins of this same stale anomaly class; closed `not_planned` with pointer to this triage.

## Why orphan reconciliation was "not firing"

It was firing all along. `reconcileOrphanClaim` (scheduler) only handles retry-exhausted in_progress beads; dead-worker recovery is Charon's `recoverOrphanInProgressBeads`, which ran 48 times today. The digest cohort ("13/19/14 orphans") snapshots the transient churn window — gate-rejected closes and provider-pool-queued respawns sit in_progress without a live row for minutes — and every recurring filing has audited to provider-pool churn or by-design escalated/pan-filed exclusions (m5j9, qqtl, v5xq, 6d01, wv2e, uzmx, ew7t, cgud).

## Disposition

No code change. Recovery chain verified live; anomaly and all secondary claims stale.
