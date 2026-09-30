# discovery handoff

## What changed (files + one line each)
- internal/core/publisher.go: pre-rebase conflict precheck — `mergeTreeConflicts` (git merge-tree --write-tree --name-only preview, returns conflicted file list) + `precheckRebaseConflicts` (predicted conflict → fail-closed error listing files + `publish_rebase_precheck_conflict` event BEFORE any rebase starts; precheck execution errors fail open). Wired into `defaultRebaseCheckoutOntoBase` (preview HEAD) and `defaultRebaseBranchOntoBase` (preview branch ref).
- internal/core/publisher_rebase_test.go: TestPrecheckRebaseConflicts_RealGit (real-git add/add conflict caught pre-rebase, clean branch passes), TestMergeTreeConflicts_ParseFileList.
- docs/discovery/publisher-rebase-precheck-mhk2.md: wedge triage + remedy.
- CHANGELOG.md: Unreleased entries (fix + triage).

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestPrecheckRebaseConflicts|TestMergeTreeConflicts|TestDefaultRebaseBranchOntoBase_RealGit|TestPublishWithRebaseRetry|TestPublishBranchViaPR_Rebase|TestPRConflict' -count=1` — PASS (10/10).
- `go vet ./internal/core/` — PASS. `gofmt -l` — clean.
- Main checkout wedge state observed and recorded (cherry-pick in progress, diverged 2/58); conflict resolution NOT automated — human-only per task.

## Open edges for the next phase
- Human must resolve the two add/add conflicts in /opt/dev-tools/components/hydra (docs/discovery/integration-wedge-triage-rsyw.md, docs/hydra/handoff/task-rsyw.md), finish the sequencer, and push.
- Precheck previews the full merge (branch tip vs origin/base); a per-commit replay conflict not visible in the full merge can still abort mid-rebase (existing fail-closed path covers it).
- New daemon binary must be deployed for the precheck to take effect (verify with scripts/verify-deploy.sh).
