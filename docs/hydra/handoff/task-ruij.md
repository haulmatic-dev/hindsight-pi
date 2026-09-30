# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-strand-triage-mhk2.md: triage of the hydra-mhk2 integration-exhausted strand — branch NOT superseded (unlanded precheck feature + cache-cap guard), merge-tree shows only 2 docs add/add conflicts, rest auto-merges; disposition = HOLD park, human rebase path documented.
- CHANGELOG.md: Unreleased/Documentation entry pointing at the triage.
- No code change (discovery/triage bead; park intentionally left standing).

## Verification (commands run + pass/fail)
- `br show hydra-mhk2` (/opt/dev-tools/components/hydra) — OPEN, P2, labels integration-exhausted + integration-failed + pan-filed; bead body says conflict resolution is human-only. PASS.
- `git fetch origin development` + `git merge-tree --write-tree origin/development hydra/wt-hydra-mhk2` — exactly 2 add/add conflicts: docs/discovery/integration-wedge-triage-rsyw.md, docs/hydra/handoff/task-rsyw.md. PASS.
- `git diff --stat origin/development...hydra/wt-hydra-mhk2` — 8 files +344/−1 (publisher precheck +51, tests +84, cache-cap-guard.sh +54, 4 docs). PASS.
- `git grep mergeTreeConflicts origin/development -- internal/core/` — no hits → feature unlanded, strand not superseded. PASS.
- Both conflict-side versions inspected with dates: origin triage doc newer (29ad719), origin handoff newer (252a68f) but branch handoff richer (pre-lint-gate-fix rsyw worker record). PASS.
- graphify-out/graph.json absent in cwd → rg/handoff fallback per protocol. PASS.

## Open edges for the next phase
- Human: rebase hydra/wt-hydra-mhk2 onto origin/development resolving the 2 doc add/adds (take origin side for the triage doc, branch side or union for task-rsyw.md), push, land via dogfood PR. Park clears only after landing.
- Precheck feature (eaf6917) + scripts/cache-cap-guard.sh are unlanded value — do NOT close the bead as superseded.
