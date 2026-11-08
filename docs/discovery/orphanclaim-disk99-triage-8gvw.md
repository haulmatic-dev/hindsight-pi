# Orphan-claim / disk-99% triage — hydra-8gvw (2026-10-06 12:42 UTC)

## Summary

Both P0 claims in the bead body are **stale at verification time**. Every orphan-reconciliation
mechanism is landed on origin/development, present in the deployed binary, and demonstrably
working today. No code change is warranted; re-landing any of them would duplicate existing work.

## Verified findings

### 1. Orphaned in_progress claims (digest: inprog=12, workers=9, orphanclaims=9)

- Live registry read `/opt/dev-tools/var/hydra/.hydra/registry.db`: all 21 in_progress hydra
  beads have a registry row; heartbeats 0s–21m old; **0 orphans** under digest semantics
  (5m claim grace, heartbeat-fresh completed rows count live, escalated/pan-filed excluded).
- Deployed binary `/opt/dev-tools/var/hydra/bin/hydra` built 2026-10-06 12:16 UTC from
  3322928 (= origin/development tip). Contains `reconcileOrphanClaim` / `orphan_claim_reset`
  (11 strings hits) and the full PR #70 → #241 → #255 fix chain.
- Charon recovery live: 44 `charon_orphan_reopened` (dead_worker) events today, latest 12:21 UTC.
- The in_progress list is dominated by the P0 anomaly meta-beads themselves (8gvw, ew7t, cgud,
  pqq5, hu12, v5xq, 0ndi) — the self-referential loop, already neutralized by the pan-filed
  exclusion in the digest (7ef09f6).
- No `P0 campaign anomaly` digest filing in today's events (2026-10-06.jsonl) — the anomaly
  stopped filing under the current binary.
- Residual escalated-orphan wedge (hydra-rxi6, hydra-q207, hydra-ee5r): escalated beads are
  in_progress with no worker by construction; Charon deliberately skips them
  (`orphan_escalated_wedged`, 15m cooldown). Resolution path exists and is wired:
  Pan `repair:clear-escalation` (one fair retry) and the revived EscalationTTLScanner
  (2f10a0f, PR #241) which now demotes in_progress escalations without a live worker after
  `overseer.escalation_ttl` (default 72h) — `SetLiveWorkerFunc` wired in overseer.go:500.
  hydra-ee5r already closed (pan-filed dedupe landed; empty-diff escalation).

### 2. Disk 99% claim

- `df -h /` → 208G/451G = **49%** (watermark 85%). Claim stale. No systemd-tmpfiles cleanup
  or cache cap needed. The diskCleanupGuard (a4d40f4, PR #158 strand) is deployed and runs
  `systemd-tmpfiles --clean` on a real breach anyway.

## Reconciliation chain (all verified LANDED on origin/development)

| Mechanism | Commit | Status |
|---|---|---|
| Scheduler reconcileOrphanClaim | bb75c4a (PR #59/#70) | LANDED |
| Digest grace + exclusions | 0a2a6c9, 7ef09f6, 0d4aeaa (PR #70) | LANDED |
| Charon park-must-not-block-recovery | 96087b0 (PR #70) | LANDED |
| Escalated digest exclusion | 7548196 (PR #144) | LANDED |
| Wedged-escalation recovery (revived TTL scanner) | 2f10a0f (PR #241) | LANDED |
| Orphan respawn mid-close + credential park auto-recovery | 3322928 (PR #255) | LANDED |

## Disposition

No code change. The recurring P0 filing class is the pre-fix digest binary + escalated-orphan
steady state; both are handled by the deployed binary. If the fleet digest fires again,
first check the deployed binary mtime vs origin/development tip (the recurring historical
root cause was a stale daemon binary), then check for escalated-labeled orphans
(`grep orphan_escalated_wedged` in the day's events).
