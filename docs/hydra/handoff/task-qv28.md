# discovery handoff (hydra-qv28)

## What changed (files + one line each)
- docs/discovery/orphanclaim-triage-qv28.md: triage — all three P0 claims (18 orphaned in_progress claims wedging the DAG ladder, publisher wedge awaiting human merge, host disk 99%) verified stale; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verification (commands run + pass/fail)
- hindsight_search at bead start — not connected (skip rule applied).
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 5 `working` rows (xcye, qv28, n9nq, 21xp, fg7e), heartbeats <30s; all `completed` rows 13:23–13:56 UTC (fresh within digest grace). Cross-checked against 28 in_progress beads → 0 live orphans. PASS.
- Events 2026-10-06.jsonl: **0** `P0 campaign anomaly` filings today; 60 orphan-path events; latest `charon_orphan_reopened` (dead_worker): dyw2 13:17, gcal 13:27, m72l 13:34 — recovery loop live. PASS.
- Publisher: `git ls-remote origin refs/heads/development` → 53eca1b = "Merge pull request #268 from dogfood/publish-hydra"; local checkout HEAD 992971f is an ancestor of 53eca1b (`git merge-base --is-ancestor` → HEAD_LANDED). Remaining open PRs (#267/#265/#262/#258/#257 DIRTY, #253/#252/#234/#232/#231 UNKNOWN) are per-bead dogfood strands in rebase conflict, not a publish PR awaiting a single human merge. PASS.
- Disk: `df -h /` → 215G/451G = **50%**, below the 85% watermark; disk-99% claim stale; no cleanup needed. PASS.
- graphify query: graph.json absent in cwd → registry/event/gh evidence per protocol.

## Open edges for the next phase
- Proposed orphan-reconciliation code change NOT warranted: scheduler reset + Charon reap/reopen + digest grace + escalated/pan-filed/gate-artifact exclusions all landed and verified live; the 18-claim snapshot was a worker-exit→respawn churn window that self-resolved.
- Per-bead DIRTY dogfood PRs remain normal integration churn owned by the rebase-retry/retest stages; no single human merge pending.
