# Integration triage: hydra-d778 and hydra-014y hard-parked (integration-exhausted)

Date: 2026-09-29. Triage run from worktree `hydra/wt-hydra-zwyy` against `origin/development`.

## Question

Two beads, `hydra-d778` and `hydra-014y`, went BLOCKED hard-parked with the
`integration-exhausted` label within minutes of each other. Per the playbook, two
beads parking with the same cause within a short window indicates a shared
integration-path wedge (a project-scoped problem like a stuck branch state or a
wedged integrate-stranded flow), not per-bead breakage. Before any park can be
cleared, the pipeline itself must be confirmed healthy — otherwise clearing the
parks just feeds the beads back into a broken path.

## Update: both branches superseded — do NOT rebase (2026-09-29, second pass)

Deeper diff inspection reversed the initial rebase recommendation. Neither
branch carries unique, still-needed work:

- **hydra-014y** (clear-escalation implementation): `origin/development` already
  contains a newer iteration of the same work — the origin version of
  `internal/core/service_starter.go` has the clear-escalation handler plus
  `escalationTTL`, `ResolveIntegrationBase`-based zero-diff checks, and the FR-030
  PR-triage service, none of which exist on the 014y branch. Rebasing 014y would
  REVERT those newer changes (`git diff origin/development hydra/wt-hydra-014y --
  internal/core/service_starter.go` shows -62/+5, deleting origin-side code).
- **hydra-d778** (FR-002 shared-infra spawn exclusivity, `exclusive_paths`):
  FR-003 "target-scope overlap serialization at spawn" (`84bffe1`) landed on
  `origin/development` with `internal/core/target_overlap.go` + scheduler wiring,
  addressing the same spawn-time overlap problem using the close-path target
  parser. `spawn_exclusivity.go` never landed and `exclusive_paths` appears
  nowhere on origin. Same line of `docs/architecture/system-loop.md` was
  rewritten by both FR-002 and FR-003, hence the conflict.

**Revised disposition:** the parks are correct and durable; the conflicts are
irrelevant because the branches are obsolete. Apply `abandon-with-summary` to
both beads citing this supersession (human/Pan-owned — the live bead store is
outside this worktree). Do NOT rebase, do NOT clear the parks for a retry.

## Original findings (first pass)

### 1. The integration path itself is healthy

- `origin/development` is advancing normally: last three landings are PR #333
  (`dogfood/hydra-24bk-retest`), the merge of `hydra/wt-hydra-24bk`, and PR #332
  (`dogfood/hydra-muvh-retest`), followed by the q1lu integration
  (`65bf797 hydra: integrate hydra/wt-hydra-q1lu`). Integrate/PR landing
  demonstrably succeeds for other beads — there is no project-wide push/merge
  failure in progress.
- Both conflict checks are reproducible and content-specific, not path-wide:

  ```
  git merge-tree --write-tree origin/development hydra/wt-hydra-d778  → exit 1
    CONFLICT (content): Merge conflict in docs/architecture/system-loop.md
    (everything else auto-merges: CHANGELOG.md, hydra-config.yaml.example,
     internal/core/config.go, internal/core/overseer.go, internal/core/scheduler.go)

  git merge-tree --write-tree origin/development hydra/wt-hydra-014y → exit 1
    CONFLICT (content): Merge conflict in internal/core/service_starter.go
    (everything else auto-merges: CHANGELOG.md, cmd/worker/main.go, internal/pan/pan.go)
  ```

  Each branch conflicts with exactly one file, in a different file from the
  other. A wedged shared path (auth failure, locked store, dead remote, stuck
  worktree) would fail identically and earlier for both; these are ordinary
  content conflicts.

### 2. Why both parked close together: both bases went stale

- `hydra-d778`'s base SHA is `65bf797` (recorded in the bead's `agent_context`),
  which was the development tip when the branch was cut. Development has since
  advanced to `5684ebe`.
- The conflicting change in each file landed on development *after* both branches
  had already exhausted their integration attempts:
  - `docs/architecture/system-loop.md` — `84bffe1` "feat(FR-003): target-scope
    overlap serialization at spawn" (2026-09-29).
  - `internal/core/service_starter.go` — `1dd9faa` "chore: stale human
    escalations blocking beads for 95–295h" (2026-09-29).
- So the near-simultaneous parking is a shared *cause* (the same merge wave
  invalidated both branches at once) but not a shared *wedge* (the pipeline
  itself was never broken — it correctly refused to merge conflicting content).

### 3. Park labels are doing their job; un-parking alone is insufficient

Per the playbook, `integration-exhausted` hard-parks are durable by design and
the scheduler retry queue, DAG ready filter, and bead discovery all skip them.
Simply clearing the park would return each bead to the same retry loop that
already burned its attempts against a base that has moved. The stranded-work
rescue path (`integrate-stranded` / FR-021, plus the rebase-retry publisher from
FR-011) exists for exactly this shape: rebase the worktree branch onto
`origin/<base>` and retry, or land via a `dogfood/<id>-retest` PR after a clean
merge-tree check.

## Conclusion (first pass; see revised disposition above)

**The integration/merge pipeline is NOT wedged.** The working branch state is
the issue: `hydra/wt-hydra-d778` and `hydra/wt-hydra-014y` are stale relative to
`origin/development` and each conflicts in exactly one file
(`docs/architecture/system-loop.md` and `internal/core/service_starter.go`
respectively). The integrate-stranded flow is functioning; the parks correctly
halted the retry loop.

## Recommended disposition (in order)

1. Rebase each branch onto `origin/development` (resolve the single-file
   conflict) or, equivalently, let the integration-retest stage (Tier A) pick
   them up — merge-tree is currently red for both, so a retest today would
   correctly keep them parked until the conflicts are resolved.
2. Only after the rebase/retest lands clean: `repair:clear-hard-park` on both
   beads (plus `repair:reset-retries` if the retry counter survived), then let
   the scheduler resume them.
3. Do NOT apply `repair:prune-worktree-registration` — that recipe targets
   shared worktree-registration wedges, and no such wedge exists here (the
   worktree branches exist and are healthy: `hydra/wt-hydra-d778` at `a3d6143`,
   `hydra/wt-hydra-014y` at `38b8ea6`).

## Evidence summary

| Check | Command | Result |
|---|---|---|
| d778 merge cleanliness | `git merge-tree --write-tree origin/development hydra/wt-hydra-d778` | exit 1, conflict in docs/architecture/system-loop.md |
| 014y merge cleanliness | `git merge-tree --write-tree origin/development hydra/wt-hydra-014y` | exit 1, conflict in internal/core/service_starter.go |
| development health | `git log --oneline origin/development -3` | PR landings present (PR #333, #332, q1lu integrate) |
| unmerged commits | `git log --oneline hydra/wt-hydra-d778 ^origin/development` | 2 commits (FR-002 work, intact) |
| unmerged commits | `git log --oneline hydra/wt-hydra-014y ^origin/development` | 1 commit (handoff/escalation chore, intact) |
| conflict provenance | `git log -1 -- system-loop.md` / `service_starter.go` | 84bffe1 / 1dd9faa landed 2026-09-29, after both branches cut |
