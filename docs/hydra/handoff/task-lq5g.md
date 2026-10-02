# discovery handoff (lq5g)

## What changed (files + one line each)
- docs/discovery/orphan-digest-pr-retry-triage-lq5g.md: triage of the orphan-digest-escalated PR-create failure — deleted head ref + wrong base, remedy executed.
- CHANGELOG.md: Documentation entry under Unreleased.
- Remote action (outside worktree): pushed `dogfood/orphan-digest-escalated` (ae68584) to origin; opened PR #147 (base development).

## Verified findings
- `git ls-remote origin refs/heads/dogfood/*` — no dogfood/orphan-digest-escalated ref existed → "Head sha can't be blank"; base dogfood/publish-hydra is an aggregate branch, not a PR base → "Base ref must be a branch".
- PR #144 (same head branch → development) MERGED at ec0fc59; remote head deleted post-merge; branch then advanced with unique commit ae68584 (fmsfe-npa spawn-failure purge: service_starter.go +65, spawn_failure_purge_test.go +29, pan.go +7, prune_sweep.go +10) — unlanded, NOT superseded.
- `git merge-tree --write-tree origin/development dogfood/orphan-digest-escalated` — CLEAN (rc=0).
- Escalated-digest exclusion (7548196) already landed via PR #144.
- Push to origin: `* [new branch] dogfood/orphan-digest-escalated` — VERIFIED.
- `gh pr create` → https://github.com/haulmatic-devteams/hydra/pull/147 — VERIFIED (daemon token).

## Open edges for the next phase
- PR #147 awaits human/admin merge (established `gh pr merge --admin` precedent). Publishing should also reset its base to development (publish-hydra is not a valid PR base).
- Publish path should not target dogfood/publish-hydra as a PR base — separate fix candidate.
