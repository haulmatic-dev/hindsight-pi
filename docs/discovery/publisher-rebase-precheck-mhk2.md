# Publisher rebase precheck — hydra-mhk2 triage

## Incident

The PR merge of hydra-rsyw's deliverables into `origin/development` failed: the
publish-time rebase attempt hit add/add conflicts in
`docs/discovery/integration-wedge-triage-rsyw.md` and
`docs/hydra/handoff/task-rsyw.md` (commit `a15231c 'docs: restore hydra-rsyw
deliverables from quarantined branch'`).

Verified state of the main checkout (`/opt/dev-tools/components/hydra`):

- `git status`: `Cherry-pick currently in progress.` — the publish path left a
  conflicted sequencer state in the daemon's own working tree.
- Branch `development` diverged from `origin/development`: 2 local / 58 remote
  commits. Locally-merged work unpublished for 1h11m at detection.

## Why it wedged

FR-011 (`publishWithRebaseRetry`) treats a PR-conflict error as a signal to
rebase and retry. The rebase itself then hit real conflicts and aborted — but
the checkout-side path can leave sequencer state behind, and every later
publish for the repo fails while the checkout is dirty. Nobody (human or
automation) resolves the conflict, so the wedge persists.

## Disposition

- Conflict resolution in the main checkout is **human-only by design** (per
  bead instruction): resolve the two add/add conflicts and
  `git rebase --continue` (or `--abort` and redo the merge), then push. Do not
  automate this.
- Code remedy implemented in this change: **pre-rebase conflict detection**.
  `git merge-tree --write-tree --name-only HEAD origin/<base>` previews the
  replay before any rebase starts. Predicted conflict → fail closed with the
  file list and a typed `publish_rebase_precheck_conflict` event; the checkout
  is never touched. Precheck execution errors fail open (log and proceed).

## Files

- `internal/core/publisher.go` — `mergeTreeConflicts`, `precheckRebaseConflicts`; called from `defaultRebaseCheckoutOntoBase` (preview `HEAD`) and `defaultRebaseBranchOntoBase` (preview the branch ref).
- `internal/core/publisher_rebase_test.go` — `TestPrecheckRebaseConflicts_RealGit` (real-git add/add conflict caught pre-rebase + clean branch passes), `TestMergeTreeConflicts_ParseFileList`.

## Verification

- `go test ./internal/core/ -run 'TestPrecheckRebaseConflicts|TestMergeTreeConflicts|TestDefaultRebaseBranchOntoBase_RealGit|TestPublishWithRebaseRetry|TestPublishBranchViaPR_Rebase|TestPRConflict' -count=1` — 10/10 PASS.
- `go vet ./internal/core/` — clean. `gofmt` — clean.
