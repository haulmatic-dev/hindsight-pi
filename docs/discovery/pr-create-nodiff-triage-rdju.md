# Triage: no-diff PR creation failure — dogfood/orphan-digest-escalated → dogfood/publish-hydra

Date: 2026-10-02. Beads: hydra-rdju (triage), hydra-ybhh (push-pending pseudo-bead, closed).

## Error (verbatim)

`GraphQL: Head sha can't be blank, Base sha can't be blank, No commits between dogfood/orphan-digest-escalated and dogfood/publish-hydra, Base ref must be a branch (createPullRequest)`

## Root cause — verified

1. `refs/heads/dogfood/orphan-digest-escalated` **does not exist on origin** (`git ls-remote origin` shows only `refs/heads/dogfood/publish-hydra`). GitHub cannot resolve the head ref → blank shas. Not transient; retry can never succeed.
2. Locally, `dogfood/orphan-digest-escalated` tip = `aab8d18` ("hydra: integrate hydra/wt-hydra-6lmz"), **identical to origin/dogfood/publish-hydra tip** — `git rev-list --left-right --count origin/dogfood/publish-hydra...dogfood/orphan-digest-escalated` = `0  0`; `git diff` empty. Even if pushed, the PR would be zero-diff.
3. Content already landed: `git merge-base --is-ancestor aab8d18 origin/development` → YES. The digest fix `7548196` (escalated-bead OrphanClaims exclusion, hydra-6d01) is present on `origin/development:internal/core/pan_health_test.go` (`TestCollectHealthDigestEscalatedBeadNotOrphan`).

Conclusion: stray local duplicate branch, never pushed; work fully integrated via the publish-hydra vehicle. No PR was ever needed.

## Disposition (executed)

- Push-pending pseudo-bead **hydra-ybhh CLOSED** (`not_planned: already-integrated/zero-diff`) with resolution comment — publisher retries for this branch stop.
- Stray local branch NOT deleted: it is the checked-out branch of the main checkout (`/opt/dev-tools/components/hydra`, via submodule gitdir `/opt/dev-tools/.git/modules/components/hydra`), which carries an **in-flight uncommitted strand** (modified `internal/core/service_starter.go`, `internal/pan/pan.go`, `internal/pan/prune_sweep.go`; untracked `internal/core/spawn_failure_purge_test.go` — a "recover-spawn-cohort" repair feature). Switching HEAD or stashing would collide with dev-tip changes to the same files. Human action required first.

## Human follow-ups

1. Land (or stash) the in-flight strand in the main checkout, then `git checkout development` and `git branch -D dogfood/orphan-digest-escalated`.
2. If the same blank-SHA class recurs for other `dogfood/*` branches, the hardening option is a publisher-side pre-check (head ref exists on origin AND differs from base before `gh pr create`); not implemented here — single occurrence, stray-branch cause.
