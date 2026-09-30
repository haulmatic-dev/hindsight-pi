# discovery handoff (hydra-isv5)

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-isv5.md: triage of hydra-isv5 integration-exhausted — publish wedge already resolved via PR #422 (b421992 merged); quarantine branch content superseded; close stands.
- CHANGELOG.md: Unreleased entry pointing at the triage.
- No code change (triage-only discovery bead; the bead was already CLOSED and its described cause no longer exists).

## Verification (commands run + pass/fail)
- `br show hydra-isv5` — CLOSED (failed), labels closed-failed/integration-exhausted/integration-failed/pan-filed. PASS (store needed `br doctor migrate-schema recover` first — "database is busy (recovery in progress)" WAL poison).
- `git merge-base --is-ancestor b421992 origin/development` → ancestor = PR #422 MERGED. PASS.
- `git merge-tree --write-tree origin/development hydra/quarantine/hydra-isv5` — 2 add/add conflicts only: docs/discovery/integration-wedge-triage-rsyw.md, docs/hydra/handoff/task-rsyw.md. PASS.
- `git cat-file -e origin/development:docs/...rsyw.md` both files — landed in newer form (252a68f + updates) → quarantine copies superseded. PASS.
- hindsight_search on integration-exhausted hard-park — durable-park-by-design + integration_retest/gate guidance confirmed. PASS.

## Open edges for the next phase
- Optional human action: `git branch -D hydra/quarantine/hydra-isv5` (only unique content is superseded rsyw docs), or leave for quarantine_auto_drain review.
- Dependents of hydra-isv5 (hydra-9yhq blocked on it) may need the dependent edge cleared since the wedge resolved.
- Docs-only add/add collisions from parallel chains remain the residual human/`retry-quarantine` case; no code change made.
