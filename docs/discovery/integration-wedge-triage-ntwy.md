# Triage: fix bead hydra-ntwy — mhk2 publisher wedge (rebase conflict blocks publishing)

## Verified findings

- `br show hydra-mhk2` — CLOSED (failed, 2026-09-30). Close note names the 2 add/add
  conflicts (docs/discovery/integration-wedge-triage-rsyw.md, docs/hydra/handoff/task-rsyw.md).
- The strand's substantive content is ALREADY on origin/development:
  - Feature commit eaf6917 (publisher pre-rebase conflict precheck) landed as
    e0a3bce via PR #443 (superset: also carries later owner-identity work).
    Handoff da5b41a documents the manual re-integration.
  - Both rsyw docs conflict files exist landed-newer on origin/development
    (29ad719 lineage).
- Rebase experiment (in hydra-ntwy worktree, detached at eaf6917, rebase onto
  origin/development): feature commit auto-dropped — "patch contents already
  upstream"; remaining docs commit conflicts only on the 2 rsyw add/add files;
  origin's versions are the newer landed ones, so the correct resolution is
  "take origin" for both — i.e. the entire strand delta is superseded.
- `git merge-tree --write-tree origin/development` against the rebased tip:
  still conflicted only on the 2 rsyw docs (when branch side is forced);
  with upstream-side resolution the merge is clean (rc=0 tree built).

## Disposition

No code change, no PR. The locally-merged work mhk2 held unpublished is
fully integrated upstream via PR #443 + landed rsyw docs. The wedge has no
remaining unpublished content; dogfood/hydra-mhk2 and
hydra/quarantine/hydra-mhk2 (eaf6917) are superseded and safe to delete
(human/Pan action, optional).

## Verification commands

- `git fetch origin development` — OK
- `git merge-tree --write-tree origin/development origin/dogfood/hydra-mhk2` — 2 add/add conflicts (reproduced original wedge)
- `git log origin/development --oneline -- internal/core/publisher.go` — e0a3bce (#443) present
- `git diff eaf6917 e0a3bce -- internal/core/` — only later landed work differs
- rebase experiment in hydra-ntwy worktree — feature commit dropped as already-upstream
