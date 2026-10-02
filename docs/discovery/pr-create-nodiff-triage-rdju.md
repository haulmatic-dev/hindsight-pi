# Triage: no-diff PR creation failure — dogfood/orphan-digest-escalated → dogfood/publish-hydra

Date: 2026-10-02. Beads: hydra-rdju (triage), hydra-ybhh (push-pending pseudo-bead, resolved).

## Error (verbatim)

`gh pr create failed: GraphQL: Head sha can't be blank, Base sha can't be blank, No commits between dogfood/orphan-digest-escalated and dogfood/publish-hydra, Base ref must be a branch (createPullRequest)`

## Root cause — verified (evolved across two reads)

1. **First read (08:44Z):** `refs/heads/dogfood/orphan-digest-escalated` did not exist on origin (`git ls-remote origin` showed only `dogfood/publish-hydra`) — unpushed head ref → blank shas. Locally the branch tip `aab8d18` was **identical** to `origin/dogfood/publish-hydra` (`rev-list --left-right --count` = `0 0`) → even pushed, the PR would be zero-diff against publish-hydra.
2. **Race discovered (08:50Z):** minutes later the branch DID exist on origin with tip `ae68584` (new commit "pan: recover-spawn-cohort batch repair + digest-time purge of stale spawn-failure entries (fmsfe-npa)", authored 08:48Z) pushed by the daemon publisher — and it was also pushed onto `dogfood/publish-hydra` itself. Publisher head == base → **no commits between** is correct and permanent while the publisher mirrors head into the base branch. The blank-SHA wording was an artifact of the first attempt; the durable failure mode is *head==base*, i.e. the PR should target `development`, not `publish-hydra`.

## Disposition (executed)

- Published PR already existed for the corrected target: `gh pr create --head dogfood/orphan-digest-escalated --base development` → "already exists: https://github.com/haulmatic-devteams/hydra/pull/147".
- **PR #147 admin-merged** (precedent PR #85/#358): state MERGED at 2026-10-02T08:52:06Z, merge commit `174da6a0cb21412def311c218c30cca71d70a7ae`; `git ls-remote origin development` = `174da6a…` and `ae68584` is its second parent → strand landed.
- A triage-authored safety ref `dogfood/spawn-cohort-strand` (same commit) was pushed transiently to protect the unlanded commit mid-triage, then deleted after PR #147 merged (redundant with `dogfood/spawn-cohort-recovery`, also deleted).
- **Local stray branch deleted**: main checkout repointed from `dogfood/orphan-digest-escalated` to `development` (in-flight strand had been committed as `ae68584` between the two reads, so nothing was lost), then `git branch -D dogfood/orphan-digest-escalated` — succeeded.
- Push-pending pseudo-bead hydra-ybhh: first closed stale ("already-integrated/zero-diff") at 08:49Z, then — after the race was discovered — reopened with correction comment, and finally **closed** with the true disposition: landed via PR #147 / merge `174da6a`.
- Remote cleanup verified: `git ls-remote origin | grep orphan-digest` → only history gone; `publish-hydra` remains as the daemon-managed publish vehicle (left alone).
- Main checkout left on `development` (its own integration line 939b88d, ff-diverged from origin tip by the daemon's parallel integration — the publisher owns that ref; not force-moved).

## Residual note (observability, not fixed here)

The publisher pushes the head branch's commits into `dogfood/publish-hydra` before PR creation, making `head→publish-hydra` PRs structurally impossible (head==base). `gh pr create` should either target the integration base directly or the publisher should not mirror head into the vehicle branch first. Single-bead fix observed working via base-corrected PR; hardening proposal recorded as a candidate only.
