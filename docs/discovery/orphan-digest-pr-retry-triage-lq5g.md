# Triage: orphan-digest-escalated PR creation failure (2026-10-02)

## Symptom

PR creation for branch `dogfood/orphan-digest-escalated` failed with:
`GraphQL: Head sha can't be blank` / `No commits between dogfood/orphan-digest-escalated and dogfood/publish-hydra` / `Base ref must be a branch`.

## Root cause

Two stacked causes, neither a permissions problem:

1. **Head ref absent.** PR #144 (dogfood/orphan-digest-escalated → development) was
   admin-merged (ec0fc59) and the remote head branch deleted post-merge. The branch
   then advanced locally with one new commit `ae68584` (fmsfe-npa spawn-failure
   purge). Any PR create against the deleted remote ref → `Head sha can't be blank`
   and `No commits between...`.
2. **Wrong base.** The publish path targeted base `dogfood/publish-hydra` — an
   aggregate publish branch, not a valid PR base → `Base ref must be a branch`.

## Verified evidence

- `git ls-remote origin refs/heads/dogfood/*` — no `dogfood/orphan-digest-escalated`
  ref; `dogfood/publish-hydra` exists at ae685844 (same tip, pushed by publish path).
- `gh pr list --head dogfood/orphan-digest-escalated --state all` → PR #144 MERGED
  (base development, head aab8d18).
- Branch delta vs origin/development = exactly ae68584: 4 files
  (internal/core/service_starter.go +65, internal/core/spawn_failure_purge_test.go +29,
  internal/pan/pan.go +7, internal/pan/prune_sweep.go +10).
- `git merge-tree --write-tree origin/development dogfood/orphan-digest-escalated`
  → CLEAN (rc=0).
- Escalated-digest exclusion fix (7548196) itself already landed via PR #144.

## Remedy executed

- Pushed `dogfood/orphan-digest-escalated` → origin (head ref restored).
- Opened PR #147 (haulmatic-devteams/hydra, base development) with triage summary.
- Merge left to human/admin per branch-protection policy (established
  `gh pr merge --admin` precedent: PRs #82, #85, #140, #144).

## Disposition

Branch is NOT empty and NOT superseded — ae68584 is a unique unlanded strand.
Correct path was re-create-PR against development with head ref restored; done.
