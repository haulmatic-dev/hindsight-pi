# Triage: hydra-isv5 integration-exhausted

## State
- `br show hydra-isv5` (store /opt/dev-tools/components/hydra/.beads, after `br doctor migrate-schema recover`): **CLOSED (failed)**. Labels: closed-failed, integration-exhausted, integration-failed, pan-filed. Bead described the hydra repo "unpublished 1h13m" with a failed PR rebase (add/add conflicts in `docs/discovery/integration-wedge-triage-rsyw.md` + `docs/hydra/handoff/task-rsyw.md`).

## Root cause (why integration passes collided)
- The publish attempt for hydra-isv5's strand (`a15231c docs: restore hydra-rsyw deliverables from quarantined branch`) had to rebase onto origin/development while **parallel strands landed updates to the same two rsyw doc files** (rsyw's own landed triage 252a68f and the rsyw handoff). Add/add conflicts in docs-only churn — classic parallel-chain collision, not a code wedge and not a push-path failure.
- No evidence of a repeated-failure storm: the bead parks once, then the wedge resolved.

## Resolution status: RESOLVED, work superseded
- The publish wedge ended: commit `ef6029b docs: publish wedge resolved — PR #422 rebased and merged (b421992)`; verified `git merge-base --is-ancestor b421992 origin/development` → MERGED (PR #422, merge commit b421992).
- Merge-tree `origin/development` vs `hydra/quarantine/hydra-isv5`: conflicts ONLY in the two rsyw docs files. Both exist on origin/development in newer/complete form (`252a68f` triage + updated handoff). The quarantine branch's copies are the older iteration — rebasing would replay stale doc content.

## Disposition
- **Do NOT clear-hard-park or reset-retries** — bead is closed; its described wedge no longer exists (publish landed).
- **Abandon-with-summary stands.** Quarantine branch `hydra/quarantine/hydra-isv5` is droppable: its only unique content is superseded rsyw docs. Optional human action: `git branch -D hydra/quarantine/hydra-isv5` (or leave for quarantine_auto_drain after review).
- Re-merge attempt would only reintroduce stale doc versions.

## Evidence
- `git log --oneline -5 origin/dogfood/hydra-isv5` — tip ef6029b (wedge-resolved note).
- `git merge-tree --write-tree origin/development hydra/quarantine/hydra-isv5` — 2 add/add conflicts, docs only.
- `git cat-file -e origin/development:docs/discovery/integration-wedge-triage-rsyw.md` / `:docs/hydra/handoff/task-rsyw.md` — both landed.
- br store WAL poison recurred ("database is busy (recovery in progress)") — recovered once; recurring known issue, not this bead's cause.

## Prevention note
The proposed remedies (serialize near-simultaneous merges / back off integration frequency) are already implemented upstream: per-repo `integrationGate` mutex + 30s cooldown (internal/core/integration_gate.go) and `publishWithRebaseRetry` fail-closed rebase cap (internal/core/publisher.go). Docs-only add/add collisions remain the residual human/Pan-retry-quarantine case.
