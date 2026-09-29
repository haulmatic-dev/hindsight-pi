# Integration wedge triage — shared integration-exhausted hard-parks (hydra-o0cn)

Bead: hydra-o0cn (discovery/triage). Task: shared wedge — many beads hard-parked
with `integration-exhausted` in the same window across the fleet
(ajantha/srv-telematics + hydra). Proposal under review: human merges/updates the
outstanding PRs (e.g. PR #394 for `dogfood/publish-hydra`); no daemon-side
automation of PR merge.

## Verified state

- PR #394 (`dogfood/publish-hydra` → `development`): **OPEN**, `mergeable: CONFLICTING`,
  not merged (verified via `gh pr view 394 --json state,mergedAt,headRefName,mergeable`).
- `git merge-tree --write-tree origin/development origin/dogfood/publish-hydra`:
  exactly one conflict — `internal/core/publisher.go`. Everything else auto-merges.
- Conflict content: overlapping edits to the same `resolveOrCreatePR` /
  `PublishViaPR` / `publishBranchViaPROnce` region.
  - On `origin/development`: fail-closed existing-PR resolver after the zpyj
    integration collision (ecc6cb5, b5eacb0, 3d6b9ef).
  - On `dogfood/publish-hydra`: the hydra-6zwl PR-reuse reconciliation
    (d6c5752) — adds `prURLFromError` fast-path that reuses the PR URL embedded
    in the `gh pr create` "already exists" error without an extra API call.
- The branch tip (d6c5752) already integrates the 6zwl work; the branch side is
  a superset for the conflict region (the fast-path builds on the same
  fail-closed resolver), so updating PR #394 with development is a small,
  human-sized conflict — comment/log wording only, not logic.

## Root cause of the wedge (already fixed daemon-side)

The hard-park cascade was caused by `gh pr create` "already exists" collisions:
every retry against an open-but-unmerged PR counted a consecutive integration
failure → `integration-exhausted` after 3. This is fixed on development
(`isPRAlreadyExists` + `resolveExistingPR` reuse, changelog 2026-09-29 entry)
and further hardened on the PR #394 branch (`prURLFromError`).

## Disposition

- Human merges/updates the outstanding PRs. For PR #394: merge development into
  `dogfood/publish-hydra` (or rebase), resolve the `publisher.go` comment/log
  overlap keeping the `prURLFromError` fast-path, then merge the PR. Direct
  push to `development` is blocked by branch protection; PR merge is the lever.
- No daemon-side automation of PR merge (per task). The PR-reuse fix already
  prevents recurrence of the park cascade.
- Do NOT clear `integration-exhausted` parks mechanically; the retest stage
  (`integration_retest`) is the automated recheck path once branches are clean.
- srv-telematics side: prior triage (hydra-501r, integration-wedge-triage-501r.md)
  stands — parks durable by design, cause environmental, operator `unpark` API
  after the repo integration path is verified.

## Open edges

- Human action required on PR #394 before the publish tick can converge.
- PR #394 branch carries the only copy of `prURLFromError`; the merge must not
  drop it.
