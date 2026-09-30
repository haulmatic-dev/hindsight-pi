# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-ut4z.md: dedupe triage — hydra-ut4z is a duplicate pan-filed filing of hydra-mhk2's publisher wedge; its fleet-concurrency proposal already adjudicated (integration_gate landed, xg6s triage PR #439 verified no capacity problem).
- CHANGELOG.md: Documentation entry under Unreleased.
- No code change (dedupe/discovery bead).

## Verification (commands run + pass/fail)
- `br show hydra-ut4z` — pan-filed, dedupe-skip title, empty body. PASS.
- `br label remove hydra-ut4z pan-filed` — ✓. PASS.
- `br close hydra-ut4z -r "not_planned: duplicate of ... hydra-mhk2 ..."` — ✓, bead CLOSED. PASS.
- `hindsight_search` on fleet-concurrency-wedge terms — per-project concurrency proposal + durable-park-by-design records confirmed. PASS.
- Landed triage `docs/discovery/integration-wedge-triage-xg6s.md` (PR #439) read from origin/development — mhk2/xxq5/5ob8 cluster NOT a capacity problem; mhk2 blocked by 2 docs add/add conflicts, human-only rebase per integration-strand-triage-mhk2. PASS.
- `git commit` in worktree — 2 files (CHANGELOG.md, triage doc), verified via `git show --stat` (gc reflog warning on a sibling worktree is unrelated). PASS.

## Open edges for the next phase
- hydra-mhk2 remains the canonical open bead: human must resolve the two rsyw docs add/add conflicts and land via dogfood PR (path documented in integration-strand-triage-mhk2.md). Do not automate conflict resolution.
- Persistent file-bead title dedupe (1e3bfed) should stop future "(dedupe skip)" twin filings; if another appears, investigate filer payload loss.
