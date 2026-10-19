# Triage: 18 orphaned in_progress claims / publisher wedge / disk 99% (hydra-qv28)

Verified 2026-10-06 13:52–13:58 UTC. All three P0 claims in the bead body are STALE.
No code change made; orphan-claim reconciliation, Charon recovery, and the publisher
path were all verified operating live.

## 1. Orphaned in_progress claims — STALE (0 live orphans)

- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 5 `working` rows
  (hydra-xcye, hydra-qv28, hydra-n9nq, hydra-21xp, hydra-fg7e), every heartbeat
  < 30 s old. 35 `completed` rows all 13:23–13:56 UTC (fresh within the 5-minute
  digest grace used by `collectHealthDigest`).
- Cross-check against `br list --status in_progress` (28 beads): every in_progress
  bead has either a live `working` row or a heartbeat-fresh `completed` row.
  Orphan claims = **0** under the digest's own semantics (5m claim grace +
  heartbeat-fresh liveness + escalated/pan-filed exclusions, all landed upstream:
  0a2a6c9, 7ef09f6, 0d4aeaa, 7548196).
- Events `2026-10-06.jsonl`: **0** `P0 campaign anomaly` filings today;
  60 orphan-path events total; latest `charon_orphan_reopened` (dead_worker):
  hydra-dyw2 13:17, hydra-gcal 13:27, hydra-m72l 13:34. Charon + scheduler
  `reconcileOrphanClaim` are actively recovering the transient churn window
  (worker exit → respawn backoff) — the "18 orphans" snapshot was taken during
  exactly that gap and has since self-resolved.
- Conclusion: no reconciliation gap exists; strengthening orphan recovery would
  duplicate the landed chain (scheduler reset, Charon reap/reopen, digest grace,
  gate-artifact and heartbeat-fresh liveness fixes). Proposed code change NOT warranted.

## 2. Publisher wedge — STALE (work published via PR #268)

- `git ls-remote origin refs/heads/development` → `53eca1b` =
  "Merge pull request #268 from haulmatic-devteams/dogfood/publish-hydra".
- Main checkout HEAD `992971f` is an ancestor of `53eca1b` → all locally-merged
  work is published.
- Remaining open PRs (#267, #265, #262, #258, #257 DIRTY; #253, #252, #234, #232,
  #231 UNKNOWN) are per-bead dogfood strands in rebase-conflict state, not a clean
  publish PR awaiting a single human merge. No publisher wedge.

## 3. Host disk 99% — STALE (50%)

- `df -h /` → 215G/451G = **50%**, far below the 85% watermark. No
  systemd-tmpfiles cleanup or cache cap required; no I/O-error-28 risk.
  `diskCleanupGuard` (a4d40f4) remains the fail-open backstop for real breaches.

## Disposition

No human merge action, no disk cleanup, no code change. The recurring P0 twin
filings of this class are the digest-filing loop's own historical output
(see 6d01 / qmf7 / wv2e / 8gvw chain); all exclusions are landed and today's
event stream shows 0 re-filings.
