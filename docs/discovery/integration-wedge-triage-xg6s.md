# Integration-exhausted shared wedge — hydra-mhk2 / hydra-xxq5 / hydra-5ob8 (2026-09-30)

Task: three beads hard-parked (or meta-triaging) with `integration-exhausted`
within minutes of each other, proposed as a shared integration bottleneck
(pipeline saturation / strand-cap limits) possibly warranting a hydra code
change.

## Verdict

NOT a live capacity problem. No concurrency raise, batching, or new circuit
breaker is warranted. Each bead's cause is individually resolved or documented
as human-only:

### hydra-mhk2 — publisher wedge, strand triaged (landed e93d270 / PR #438)

- The only bead still carrying an open `integration-exhausted` label in the
  hydra store.
- NOT superseded: `eaf6917` (publisher pre-rebase conflict precheck,
  `mergeTreeConflicts`/`precheckRebaseConflicts`) and
  `scripts/cache-cap-guard.sh` are real, unlanded features — absent from
  origin/development (verified via `git grep mergeTreeConflicts
  origin/development -- internal/core/` → no hits).
- `git merge-tree --write-tree origin/development hydra/wt-hydra-mhk2` →
  exactly 2 add/add conflicts, both docs-only:
  `docs/discovery/integration-wedge-triage-rsyw.md` and
  `docs/hydra/handoff/task-rsyw.md`. `internal/core/publisher.go` auto-merges.
- Full resolution path documented in
  `docs/discovery/integration-strand-triage-mhk2.md` (on origin/development):
  take origin side of the triage report, branch side (superset) of the
  handoff, rebase, land via dogfood PR. Human-only repo-content operation —
  the pan-filed bead body itself forbids automating conflict resolution.
- Park held per that triage; clearing it before the rebase lands would loop.

### hydra-xxq5 — mass-park meta bead (18 beads, ~7-minute window)

- Constituent park causes were all fixed and drained by prior triages:
  stale integration base (PR #340, s402), PR-already-exists retry loop
  (`resolveOrCreatePR`, PR #418 + hydra-8odx state-read retry), poisoned br
  WAL index (`br doctor migrate-schema recover`), isPRAlreadyExists
  redeclaration lint break (e0c70a6 — which also fixed this bead's own gate
  failures).
- hydra store scan: zero open beads carrying `integration-exhausted` other
  than hydra-mhk2. Nothing left to batch-clear.
- Disposition: closeable as cause-fixed; do not clear parks that no longer
  exist.

### hydra-5ob8 — fleet-wide meta bead (uqxe, y7p9, 242b, srv-telematics cluster)

- Proposal inside the bead ("re-verify the PR-already-exists fallback landed")
  is satisfied: `resolveOrCreatePR` + `isPRAlreadyExists` landed via PR #418
  (merge 8ad10d8) and the post-merge `gh pr view` state-read retry
  (hydra-8odx fix) landed separately.
- Prior handoff (task-fhhy) already recorded: "can be closed as cause-fixed
  once its own chain verdict lands." Its named triggers (PR #389, disk 99%,
  husky lint wedge) are all verified resolved in that triage.
- srv-telematics store: zero open `integration-exhausted` beads (fhhy triage
  scan).

## Why no code change

- The code-change proposal in the task ("raise integration concurrency for
  drain-through / batching reconciliation") targets a saturation problem that
  the evidence does not show: PR #438/#437 landings on origin/development
  demonstrate a healthy pipeline; the only blocked strand (mhk2) is blocked by
  two docs add/add conflicts, not by capacity.
- The mhk2 branch itself already contains the next useful code change
  (pre-rebase conflict precheck, fail-closed) — implementing it here would be
  a duplicate of an unlanded strand. Land that strand instead.
- Hard-park labels are durable by design; mass-clearing without cause-fix
  re-parks (recorded in hindsight: ykz, dgv recurrences). No park-clearing
  action taken.

## Actions taken

- No live-store mutation beyond this triage comment (none required; all three
  beads already carry prior triage comments, and mhk2's park is intentionally
  held).
- No code change. Triage-only deliverable: this report + CHANGELOG entry.

## Verification (commands run)

- `hindsight_search` on shared-wedge terms — durable-park-by-design,
  prune-worktree-registration recipe, circuit-breaker cap 3/3 records confirm
  prior playbook. PASS.
- `br show hydra-mhk2 / hydra-xxq5 / hydra-5ob8` — statuses, labels, bodies as
  described above. PASS.
- hydra store scan (`br list --json`): only hydra-mhk2 is an open bead with
  `integration-exhausted`. PASS.
- `git cat-file -e origin/development:docs/{discovery/integration-wedge-triage-rsyw.md,hydra/handoff/task-rsyw.md}`
  — both landed. PASS.
- `git show origin/development:docs/discovery/integration-strand-triage-mhk2.md`
  — landed triage with per-file resolution path. PASS.
- `git merge-tree --write-tree origin/development hydra/wt-hydra-mhk2` — rc=1,
  2 add/add doc conflicts (consistent with landed triage). PASS.
- `graphify-out/graph.json` absent in cwd → rg/handoff fallback per protocol.
  No code change, so impact analysis N/A.
