# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-d71g.md: full triage of the 21+ bead mass integration-exhausted wedge — root cause (stale daemon binary predating PR-reuse fixes), phase-2 residual duplicate-iteration collisions, store inventory, disposition.
- CHANGELOG.md: Unreleased entry pointing at the triage.
- No code change (cause already fixed and deployed; verified in production logs).

## Verification (commands run + pass/fail)
- `grep 'already exists' /opt/dev-tools/var/hydra/events/2026-09-29.jsonl` — 1380 hits, zero after 16:57 UTC (binary rebuild time). PASS.
- `/var/log/hydra/hydra-error.log` — `[Publish] PR for dogfood/ajantha-srv-telematics-5y3v already exists — reusing PR from create error: https://github.com/haulmatic-dev/srv-t...` at 17:27:47 (plus df0w ×2, 4p14). Fix confirmed live. PASS.
- srv-telematics store SQLite (`labels join issues`) — 132 `integration-exhausted`, 0 open (all closed). Nothing to batch-clear. PASS.
- hydra store SQLite — exactly 1 open bead with the label: hydra-15zh (pan-filed wedge meta bead, gate-failed on the since-landed dedupe). PASS.
- `git merge-tree --write-tree origin/development hydra/wt-hydra-{o0cn,pmed,4axh,15zh,uqxe,7ywm,xxq5}` — all CONFLICT in internal/core/publisher.go; delta vs development is only d6c5752's `prURLFromError` fast-path + comment churn (superseded duplicates; reconciled combination exists as local checkout 9a905d3, `go build ./...` PASS). PASS.
- Post-restart integration-failed events (24) — all genuine rebase conflicts (jest.config.ts in srv-telematics ×44 pre-restart; publisher.go duplicate-iteration in hydra), no `already exists` recurrence. PASS.
- Live store: closed hydra-15zh as superseded (dedupe fix landed via ecc6cb5/PR #393 lineage; 6zwl content running in daemon). PASS.

## Open edges for the next phase
- Land `dogfood/publish-hydra` PR #394 (9a905d3 reconciliation) — makes future wt-branch rebases conflict-free for the 6zwl lineage.
- Tier B agentic conflict resolution still unimplemented — the durable fix for duplicate-iteration collisions; separate bead (qzxv/xlkw).
- srv-telematics rebase log noise: `skipped previously applied commit 99ec632c` on most branches; consider fork-point handling in rebaseBranchOntoBase.
