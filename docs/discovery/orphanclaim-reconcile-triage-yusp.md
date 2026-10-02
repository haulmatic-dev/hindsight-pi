# Orphan-claim reconcile triage (hydra-yusp, 2026-10-02)

Fleet digest: hydra inprog=4, orphanclaims=1. Bead proposes scheduler orphan-claim
reconciliation / Charon recovery investigation (claim lease expiry, heartbeat
staleness window). Findings: the mechanical fixes are ALREADY LANDED; the anomaly
is the digest filing loop restarting under a stale daemon binary.

## Landed machinery (verified on origin/development)
- `internal/core/scheduler.go` `reconcileOrphanClaim`: retry-exhausted in_progress
  bead with no live registry worker and no hard-park label -> reset to open,
  retry entry cleared (one fair retry), typed `orphan_claim_reset` event. This is
  the scheduler-side reconcile the bead asks for. Runs when the bead is neither
  DAG-ready nor retry-eligible, so it is invisible-wedge-safe.
- `internal/core/charon.go` `recoverOrphanInProgressBeads`: in_progress + no live
  worker for > `orphanBeadGrace` (5m) -> reopen to open. Liveness =
  live tracked PID OR registry heartbeat fresher than `heartbeatHungAfter` (5m).
  Fresh canonical re-read + escalated/integration-exhausted/worktree-unavailable
  label skips (fail-closed on destructive reopen; zero UpdatedAt skipped).
- Digest counter (`internal/core/pan_health.go`): `orphanClaimDigestGrace` 5m
  (fresh claim skipped — spawn/claim/registry-upsert is not atomic),
  heartbeat-fresh completed rows count as live, pan-filed meta beads excluded
  from inprog. Fixes the false-positive filing loop (ctix→qmf7→t2e1→yz8z twins,
  13 min, 2026-10-02 02:34–02:48).

## Why the reconcile did not fire
Not a code gap: the DEPLOYED daemon (`/opt/dev-tools/var/hydra/bin/hydra`, PID
250496, started 2026-10-02 00:47:23 UTC) predates the landed fixes (PR #70,
merge 5efedd9: 0a2a6c9 grace, 7ef09f6 pan-filed exclusion, 0d4aeaa heartbeat
liveness). The twin triage hydra-x60p staged a new binary; the staging dir is
now empty and `bin/hydra` (mtime 03:32, contains `orphan_claim_reset`) matches
the new build — a daemon restart is the only remaining step.

## Live state at 03:35 UTC (verified)
- registry.db: 7 rows; 6 `working` with fresh heartbeats (03:35:54–03:36:10),
  1 completed — ZERO orphan claims right now.
- in_progress set = this bead + 3 sibling anomaly twins (1lgg, rcve→closed,
  x60p) + hydra-l867 (pan-filed meta about ns8y hard-park, gate-failed build
  close-reject). l867's worker row completed fresh at 03:37 — its closure path
  is the verified-close gate, not orphan recovery.
- Events 2026-10-02.jsonl: Charon DID recover real orphans (fmsfe-sn5 00:52,
  hydra-1p1q 02:45, hydra-vk94 02:47) — recovery loop works.
- Disk: `df -h /` = 166G/451G = 39% — the 99% claim is stale; no
  systemd-tmpfiles cleanup required, no ENOSPC risk.

## Disposition
- No code change required: all proposed mechanisms exist on origin/development.
- Remaining action (operator-only, restarts the fleet including live workers):
  restart the hydra daemon so `bin/hydra` (post-PR#70 build) goes live. Do not
  restart while P0 twin workers are mid-turn.
- Pan should not file further orphan-claim twins; the digest grace + exclusions
  now suppress the loop.
