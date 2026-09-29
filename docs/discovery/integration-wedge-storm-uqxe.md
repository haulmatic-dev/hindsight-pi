# Integration-exhausted hard-park storm — shared wedge triage (hydra-uqxe)

Bead: hydra-uqxe · 2026-09-29 · evidence: `/opt/dev-tools/var/hydra/events/*.jsonl`
(2,090 `integration-exhausted` events, 166 unique beads, 2026-09-17 → 2026-09-29).

## Summary

The storm is real but NOT serialized rebase/merge contention on a shared working
branch. Event-log forensics show three distinct systemic failure modes; two are
already fixed on `development`, one is fixed by this bead.

## Failure-mode breakdown

### 1. PR-already-exists retry loop (2026-09-29 storm, 49 beads, 276 events)

Every close for srv-telematics failed at `gh pr create` with:

```
a pull request for branch "dogfood/<bead>" into branch "feat/intergration-test-plan" already exists:
https://github.com/haulmatic-dev/srv-telematics/pull/293
```

Root cause: an earlier publish attempt created the PR but failed before merging
it; every retry re-ran `gh pr create` on the same head, got the same rejection,
counted it as an integration failure, and exhausted into `integration-exhausted`
hard-park. `defaultAutoPRGhCreate` had no already-exists handling — the retry
loop was guaranteed. **Fix:** landed on development via the publisher PR-reuse
reconciliation (hydra-6zwl): `resolveOrCreatePR` + `prURLFromError` in
`internal/core/publisher.go` — reuses the stranded PR (URL from the create
error, or `gh pr view` fallback) and continues to the merge. Regression tests:
`internal/core/publisher_pr_exists_test.go` (adapted to the landed seams).

### 2. Pre-push hook lint failure (2026-09-27 storm, 25+ beads, 903 events)

All dogfood pushes to `feat/intergration-test-plan` died in the repo's husky
`pre-push` hook (`lint:ci` eslint failure). **Already fixed on development:**
`defaultPRMergeBranchPush` / `defaultAutoPRGitPush` push with `--no-verify`
(comment cites the 2026-09-27 telematics incident). No further work.

### 3. Merge conflicts + git index.lock contention (355 + scattered events)

- `git merge --no-ff` conflicts concentrated in shared churn files:
  `docs/hydra/handoff/discovery.md` (313), `jest.config.ts` (177),
  `test/setup/env.ts` (173) — expected TDD-chain churn, correctly parked.
- `index.lock` collisions (`Another git process seems to be running`) in
  `/home/ajantha/projects/srv-telematics` worktrees — concurrent daemon git
  processes on the same repo. Transient; retest resolves.

## Dispositions

- No `repair:clear-hard-park` on any currently parked integration-exhausted
  bead whose branch still conflicts — retest stage
  (`internal/core/integration_retest.go`, enabled) handles provably-clean ones
  mechanically each tick.
- Beads hard-parked on PR-already-exists with clean merge-tree are now
  recoverable: with the publisher fix landed, the retest stage's PublishViaPR
  path will resolve-and-merge the stranded PRs automatically.
- `pmgh` integrate-stranded quarantine (merge conflict): Tier B agentic
  conflict resolution remains unimplemented (separate bead); human merge.
- SECURITY note: integration-exhausted event details embed the full
  authenticated remote URL including the GitHub PAT (x-access-token:…). The
  push path calls `redactToken`, but the integration failure formatter
  interpolates the raw git error. Worth a follow-up to redact at the
  integrate-failure site.

## Timeline evidence

- 2026-09-25: 184 events / 39 beads — merge-conflict wedge on handoff docs.
- 2026-09-27: 1,264 events / 54 beads — pre-push lint wedge (fixed) + conflicts.
- 2026-09-29: 276 events / 49 beads — PR-already-exists wedge (fixed here).
