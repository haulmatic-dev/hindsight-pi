# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-8py6.md: triage of the multi-bead integration-exhausted accumulation (hydra-242b, hydra-ycmu, srv-telematics cluster) — root cause (human-only quarantine of stranded-branch conflicts + nothing drains), 145 labels/91 quarantine branches evidence, two code-change proposals.
- CHANGELOG.md: Unreleased entry pointing at the triage.
- docs/hydra/handoff/task-8py6.md: this handoff.
- No code change (discovery bead; parks intentionally left standing per task).

## Verification (commands run + pass/fail)
- `br show hydra-242b` (/opt/dev-tools/components/hydra) — OPEN, triage-superseded label + ng18 triage comment already present; nothing new needed. PASS.
- `br show hydra-ycmu` — OPEN, pan-filed duplicate-class bead for the already-fixed escalation wedge. PASS.
- srv-telematics store (`/home/ajantha/projects/srv-telematics`): direct SQLite read — 145 issues with `integration-exhausted`, 62 of them status=closed; 91 `hydra/quarantine/*` branches via `git branch`. PASS.
- Per-bead shows (i6zt, 5l11, 04jq, f10l, burr, nygw, kher, 5gq) — all confirm quarantine/pan-filed wedge pattern; 5gq quarantines pmgh; i6zt quarantines 5gq (quarantine-of-quarantine). PASS (intermittent `Database error: database is busy (recovery in progress)` WAL poison; `br doctor migrate-schema recover` succeeded twice, third attempt failed at live-recovery under daemon concurrency).
- No merge-tree checks needed for ajantha beads — project path outside allowed writes; evidence taken from store + labels.

## Open edges for the next phase
- Implement bead 1: clear quarantine label set ({integration-exhausted, integration-failed, stranded-conflict, stranded-work, integration-unverified, worktree-unavailable}) on terminal close — 62 ghost labels in srv-telematics alone.
- Implement bead 2: quarantine-branch retention cap / auto-drain of quarantine branches whose owning bead is closed superseded/abandoned (91 branches and growing).
- Human: resolve/drop the srv-telematics quarantine backlog; run `br doctor migrate-schema recover` on that store during a daemon-quiet window.
- hydra-ycmu: abandon-with-summary; park stands.
