# DISCOVERY handoff

## What changed (files + one line each)
- docs/hydra/handoff/task-mibl.md — this document: integration-exhausted state of hydra-d778 diagnosed; no code changes (integration is human-only).

## Verified findings (hydra-d778 integration state)

Work status: COMPLETE, not lost.
- Branch `hydra/wt-hydra-d778` holds 2 commits, pushed already (origin/dogfood/hydra-d778 == worktree HEAD):
  - ae11a67 feat(FR-002): shared-infra spawn exclusivity lock
  - a3d6143 docs: task handoff for hydra-d778 (FR-002 spawn exclusivity lock)
- FR-002 accepted (outcome pattern: done, 4 AC met); worker verdict recorded full `go test ./...` PASS.
- Worktree exists: `/opt/dev-tools/var/hydra/.hydra-worktrees/-opt-dev-tools-components-hydra/hydra-d778`.

Root cause of integration-exhausted (from events, 2026-09-29):
1. PR already exists: https://github.com/haulmatic-dev/hydra/pull/316 (dogfood/hydra-d778 → development). Publisher retried `gh pr create`, which fails with:
   `a pull request for branch "dogfood/hydra-d778" into branch "development" already exists` → 4 consecutive integration failures → `integration-exhausted` (consecutive_failures_capped) → hard-park.
2. `integration_retest` confirmed: `branch hydra/wt-hydra-d778 still conflicts with origin/development — stays parked (Tier B agentic resolution not implemented)`.
3. Conflict files vs origin/development (merge-tree): CHANGELOG.md, docs/architecture/system-loop.md, hydra-config.yaml.example, internal/core/config.go — concurrent merged PRs (FR-032 etc.) touched same files.

## Required human action (agent cannot do this)
1. Update the stale PR branch so PR #316 is mergeable:
   ```
   cd /opt/dev-tools/components/hydra
   git checkout -B dogfood/hydra-d778 origin/dogfood/hydra-d778
   git fetch origin
   git rebase origin/development        # or merge; resolve the 4 files above
   go build ./... && go test ./internal/core/ ./pkg/types/
   git push --force-with-lease origin dogfood/hydra-d778
   ```
2. Merge PR #316 (branch protection on development ⇒ `gh pr merge 316 --admin` per house convention).
3. Clear the hard park: `repair:clear-hard-park hydra-d778` (only after PR merged — the durable label is by design).
4. Delete stale worktree `.../hydra-d778` and branch `hydra/wt-hydra-d778` after merge.

## Verification (commands run + pass/fail)
- `git log origin/dogfood/hydra-d778..hydra/wt-hydra-d778` — empty (work fully pushed). PASS.
- events JSONL: integration-failed ×4 + integration-exhausted + integration_retest(conflict) entries read. VERIFIED.
- merge-tree conflict list derived from actual git objects. VERIFIED.
- No code/build changes made; no tests to run.

## Open edges for the next phase
- Consider fixing the publisher: on `pr create` "already exists" it should fall through to PR update/merge instead of counting as integration failure (root-cause fix, prevents recurrence).
- Tier B agentic conflict resolution for integration-retest is unimplemented (event says so); rebase-on-conflict would have auto-recovered this bead.
