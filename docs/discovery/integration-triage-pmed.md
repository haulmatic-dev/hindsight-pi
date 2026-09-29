# Integration triage: hydra-pmed (integration-exhausted hard-park)

Date: 2026-09-29. Bead: hydra-pmed (pan-filed task, IN_PROGRESS) → pan-filed hydra-3jg0 executed this triage.

## Bead state

- `br show hydra-pmed` (hydra store): labels `integration-exhausted, integration-failed, pan-filed`, status IN_PROGRESS.
- Bead body: push-pending — rebase of `hydra/wt-hydra-pmed` onto origin/development failed mid-rebase at commit d6c5752 ("hydra: integrate hydra/wt-hydra-6zwl (publisher PR-reuse reconciliation)"):
  - `CONFLICT (content): Merge conflict in internal/core/publisher.go`
  - `CONFLICT (modify/delete): internal/core/publisher_existing_pr_test.go deleted in HEAD and modified in d6c5752`
- Event stream (`/opt/dev-tools/var/hydra/events/2026-09-29.jsonl`): `integration-exhausted` on hydra-pmed at 17:54:49Z, `consecutive_failures_capped`, 5 consecutive integration failures, last at integrate via `dogfood/hydra-pmed → development: pr conflict: rebase attempt 1 failed`, `conflict_paths: ["internal/core/publisher.go"]`.

## Root cause

A transient colliding change on the base branch: d6c5752 (the 6zwl publisher PR-reuse reconciliation integration) touched `internal/core/publisher.go` and deleted `internal/core/publisher_existing_pr_test.go` while concurrent branches (pmed and siblings) also carried publisher.go edits. Rebase of the stale branch onto origin/development conflicted; after the rebase-retry cap the bead hard-parked.

## Is the cause still live? NO.

Verified 2026-09-29, after `git fetch origin`:

- `git merge-tree --write-tree origin/development hydra/wt-hydra-pmed` → **exit 0, no conflicts**. The branch now merges cleanly.
- `git log origin/development..hydra/wt-hydra-pmed` → 1 unique commit (59a95c5, docs-only triage handoff). No feature code remains to integrate.
- Upstream resolution: commit ecc6cb5 "fix(publisher): restore fail-closed existing-PR resolver after zpyj integration collision" landed the publisher reconciliation on development; 69a6cb8 (srv-telematics PR-already-exists reuse) is an ancestor of origin/development (verified via `git merge-base --is-ancestor`).
- Pipeline healthy: origin/development tip dcaa38c (PR #397 merge), reflog shows fast-forward-only fetches — no remote rewind, no broken integration step.

## Shared-wedge framing (hydra-pmed / hydra-242b / srv-telematics-burr)

The three parks within minutes are NOT one common conflicting change:

- hydra-242b: superseded feature (config.go escalation-knob conflict; already dispositioned abandon-with-summary).
- srv-telematics cluster: project-local push-path blockage (per docs/discovery/integration-wedge-triage-501r.md).
- hydra-pmed: publisher.go collision with d6c5752, now resolved upstream.

No systemic base-branch breakage found; per-bead hygiene/triage is the correct disposition for pmed too.

## Disposition / recommendation

Cause no longer persists. Recommended repair path (human or Pan, in the live store):

1. `repair:clear-hard-park` on hydra-pmed (removes integration-exhausted/integration-failed labels).
2. `repair:reset-retries` for one fair retry — scheduler re-integration should now succeed (clean merge-tree), closing the push-pending state; the branch's only remaining commit is docs-only.

Do NOT rebase by hand, do NOT delete the branch, and do NOT clear the park before the labels-removal path above is chosen deliberately. The stale cherry-pick state in the local main checkout (CHERRY_PICK_HEAD 9a905d3, "hydra: integrate hydra/wt-hydra-uewg") is a separate repo-state issue, out of scope here — recorded as an open edge.
