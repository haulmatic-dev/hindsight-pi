# red handoff

## What changed (files + one line each)
- internal/core/publisher.go: new `branchMergedIntoBase(projectPath, branch)` — true when the working branch tip is an ancestor of `origin/<integration base>` (fail-open on any resolution/git error); `PublishRepo` skips the publish with benign reason "already integrated" and a typed `publish_push_skipped_merged` event when the branch tip is already merged upstream.
- internal/core/overseer.go: "already integrated" added to `benignPublishReasons` — a skipped publish clears push-pending instead of re-noting it.
- internal/core/publisher_merged_base_test.go: real-git regression test — merged+pruned branch reported integrated, unmerged branch not, base==branch and empty inputs fail-open.
- CHANGELOG.md: Fixed-style entry under Unreleased.

## Root cause (verified)
- Main checkout `/opt/dev-tools/components/hydra` sits on working branch `dogfood/orphan-digest-escalated`, tip aab8d18. `git merge-base --is-ancestor aab8d18 ec0fc59` → YES: the whole strand already landed on origin/development via PR #144 (merge of dogfood/orphan-digest-escalated). The remote branch `dogfood/orphan-digest-escalated` was pruned after that merge (`git ls-remote origin` shows only dogfood/publish-hydra = aab8d18).
- The publish tick kept firing because `branchAheadOfRemote` sees the missing `origin/dogfood/orphan-digest-escalated` ref → "ahead" → full publish every tick. PublishViaPR pushed HEAD to dogfood/publish-hydra, then `gh pr create --base dogfood/orphan-digest-escalated --head dogfood/publish-hydra` failed deterministically: "GraphQL: Head sha can't be blank, Base sha can't be blank, No commits between dogfood/orphan-digest-escalated and dogfood/publish-hydra, Base ref must be a branch (createPullRequest)" — head branch gone from remote, zero commits between tips anyway. Events 2026-10-02.jsonl show the same publish_path_classified UNKNOWN loop (08:45:15→08:45:32 within 17s).
- The fix makes the publisher recognize "tip already merged into origin base" and skip instead of attempting a no-op PR.

## Verification (commands run + pass/fail)
- `gofmt -l` on touched files — clean.
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run 'TestBranchMergedIntoBase|TestPublishRepo' -count=1` — PASS.

## Open edges for the next phase
- The local checkout remains on the merged branch `dogfood/orphan-digest-escalated`; with the fix the daemon will report "already integrated" (benign → push-pending cleared). Hygiene follow-up for a human: switch the main checkout back to development and delete the local merged branch.
- `resolveOrCreatePR` still allows PR create with a pruned head branch for the PR-first close path; the same blank-sha failure class can appear there. A classifier for the blank-sha/zero-commit GraphQL error (own PublishPathClass, distinct from UNKNOWN) is a candidate follow-up.
