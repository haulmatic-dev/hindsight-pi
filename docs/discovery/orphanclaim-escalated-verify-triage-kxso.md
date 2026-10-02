# Triage verification: P0 campaign anomaly — 4 in_progress beads / disk 99% (hydra-kxso)

Verified 2026-10-02 08:28–08:40 UTC. This bead is a verification/duplicate of
`hydra-6d01` (triage: docs/discovery/orphanclaim-escalated-triage-6d01.md,
fix landed as commit 7548196 on origin/development via the publish PR chain,
tip 55102f3). No code change.

## Claim 1: "4 in_progress beads with no live registry worker" — self-resolved / by design

- The 4 beads: hydra-wiu6, hydra-1h4v, hydra-bxzr, hydra-cxpf — all carry
  `escalated`, `needs-human`, `verified-close-escalated`,
  `verified-close-reject-tdd-validation-failed`. Verified via `br show`.
  They stay in_progress by design pending human/Pan review; not orphaned.
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` at 08:29 UTC: 6
  `working` rows, every heartbeat < 1 min old, PIDs live (`ps`). No live
  orphan. f4oa/65wl/6d01/t1iz rows state=completed with fresh heartbeats —
  counted live per the heartbeat-fresh liveness rule.
- Charon sweep live-verified: 52 `charon_orphan_reopened` events today
  (dead_worker), incl. bxzr 07:47, cxpf 07:50, hydra-kxso itself 08:24 —
  recovery loop operating.
- No `orphanclaims>` or `P0 campaign anomaly` digest filing in today's
  events after the 6d01 window — anomaly not currently re-firing.
- Code fix for the false-alarm class already landed:
  `7548196 fix(digest): exclude human-escalated beads from OrphanClaims
  counter` (internal/core/pan_health.go:177) on origin/development.
- 21 wtbranches: normal churn; re-run digest after 6d01 restart, no
  stranded-work cleanup indicated.

## Claim 2: disk 99% — STALE

- `df -h /` → 188G/451G = **44%**, below the 85% watermark. No
  systemd-tmpfiles cleanup, no br I/O-error-28 risk.

## Residual open edge: deployed daemon binary is stale

- `/opt/dev-tools/var/hydra/bin/hydra` (mtime 07:44, running since 08:19)
  LACKS: `orphan_claim_reset` literal (PR #70 scheduler reconcile),
  `empty_diff_completed` (empty-diff gate), and the 7548196
  escalated-exclusion. It has `charon_orphan_reopened`, `escalation-expired`
  (older fixes) — so it predates today's PR #70 chain.
- Worker binary mtime 00:44 — also predates.
- Fresh binaries built from HEAD (7548196, = origin/development tip
  minus merge commit) in this worktree, VERIFIED present:
  - `/opt/dev-tools/var/hydra/.hydra-worktrees/-opt-dev-tools-components-hydra/hydra-kxso/hydra-staged-kxso`
    (`orphan_claim_reset` literal present, `go build ./...` PASS)
  - `.../hydra-kxso/worker-staged-kxso`
- Action for operator: stop daemon (drain), copy the staged binaries over
  `$HYDRA_HOME/bin/{hydra,worker}`, restart. Until then the escalated
  in_progress beads (wiu6/1h4v/bxzr/cxpf) still need the human
  verified-close review regardless of binary version.

## Disposition

No code change; both anomaly claims stale/by-design; escalate the stale
binary deploy to the operator.
