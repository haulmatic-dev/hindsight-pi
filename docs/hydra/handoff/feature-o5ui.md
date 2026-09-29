# FEATURE handoff

## What changed (files + one line each)
- internal/core/publisher.go: FR-011 — `prConflictError` detector (conflict / not-mergeable phrasings), `MaxPRRebaseAttempts = 2`, `rebaseCheckoutOntoBase` (rebase working branch onto origin/<base>, `--autostash`, hooks disabled), `rebaseBranchOntoBase` (scratch detached worktree checked out at the branch, rebase onto origin/<base>, `update-ref` moves branch; no checkout touched), `publishWithRebaseRetry` (conflict → rebase → retry, cap 2; rebase failure or still-conflicted after cap → fail-closed "human review needed" error + `publish_rebase_attempted`/`publish_rebase_exhausted` events). `PublishRepo` pr_merge path and `PublishBranchViaPR` (renamed single attempt to `publishBranchViaPROnce`) wired.
- internal/core/publisher_rebase_test.go: 10 tests — conflict detection table, rebase+retry success, cap-2-then-human, failed-rebase escalates immediately, non-conflict error passthrough, PublishBranchViaPR rebase retry + exhaustion, real-git scratch rebase integration test.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS
- `go vet ./internal/core/` — PASS (clean)
- `go test ./internal/core/ -run 'TestDefaultRebaseBranchOntoBase_RealGit|TestPRConflict|TestPublishWithRebaseRetry|TestPublishBranchViaPR_Rebase' -count=1` — PASS (ok, 8+ subtests)

## Open edges for the next phase
- `maybeOpenAutoPR` (bead-level auto-PR, head `dogfood/<id>`) still fails open on conflict without rebase — its head branch lives on origin, rebasing it needs a remote-tracking rebase path not yet built; conflicts there land in the existing quarantine flow.
- Scratch worktrees live in os.TempDir(); a crashed run can leave `hydra-rebase-*` worktree registrations until `worktree prune`.
- `rebaseBranchOntoBase` moves the branch ref behind any worktree that has the branch checked out (its index goes stale); safe for daemon-owned branches only, as documented.
- Key bug found during implementation (worth a CASS rule): `git rebase <upstream>` rebases the CURRENT branch onto <upstream> — the first helper version checked out the scratch at origin/<base> and rebased the feature branch, inverting the direction; only the ancestry assertion caught it (merge-base == base tip is the right check, not "rebase exited 0").
