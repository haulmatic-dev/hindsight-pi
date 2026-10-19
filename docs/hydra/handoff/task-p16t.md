# discovery handoff (hydra-p16t)

## What changed (files + one line each)
- docs/discovery/orphanclaim-publisher-disk-triage-p16t.md: triage — all three P0 claims (19 orphaned claims, publisher wedge, disk 99%) verified stale/self-resolved; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verified findings
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 6 live `working` rows, heartbeats <30s (incl. hydra-p16t=self); every non-live in_progress bead holds a recent `completed` row → retry-gap churn snapshot, 0 live orphans. PASS.
- `br list --status in_progress`: 10 in_progress (not 19). Events 2026-10-06.jsonl: 48 `charon_orphan_reopened`, 0 `P0 campaign anomaly` filings today. Recovery loop LIVE. PASS.
- Publisher: local checkout HEAD f6ec43c IS ancestor of origin/development; PR #269 (dogfood/publish-hydra → development) MERGED 2026-10-06T14:14:44Z, mergeCommit 5453080 = `git ls-remote origin development` tip. Work published; no human action outstanding. PASS.
- Disk: `df -h /` → 167G/451G = **39%** (watermark 85%) — 99% claim stale. `systemd-tmpfiles --clean` run. PASS.
- `git merge-tree --write-tree origin/development origin/dogfood/publish-hydra` — clean (rc=0). PASS.

## Open edges for the next phase
- Main checkout auto-gc failing (`fatal: failed to run reflog` / `error: task 'gc' failed`) — candidate hygiene bead.
- Stale CONFLICTING per-bead dogfood PRs (#252/253/257/258/262/265/267…) remain open — cleanup candidates.
