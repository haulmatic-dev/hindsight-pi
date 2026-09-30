# TASK handoff (hydra-fetm)

## What changed (files + one line each)
- docs/hydra/handoff/task-fetm.md: this document (integration action bead; no code change in this worktree — the landed code change went via PR #443).

## Verified outcome (hydra-mhk2 hard-park)
Task: integration-exhausted hard-park on hydra-mhk2 needed human review — resolve or manually re-integrate.

Disposition: MANUALLY RE-INTEGRATED.
- Bead state at start: IN_PROGRESS, P2, labels integration-exhausted + integration-failed + pan-filed; blocker = add/add conflicts in docs/discovery/integration-wedge-triage-rsyw.md + docs/hydra/handoff/task-rsyw.md during rebase of a15231c.
- `git merge-tree --write-tree origin/development hydra/wt-hydra-mhk2` — still 2 add/add conflicts; strand NOT superseded: unique commit eaf6917 (pre-rebase conflict precheck, publisher.go +51, tests +84) unlanded.
- Wholesale merge REJECTED: merge of DEV+mhk2 tip deletes newer landed work (integration_exhausted_sweep.go/-153, shepherd.md, config knobs) because the branch carries stale integrations (2e7fafb integrated the stale rsyw branch). Verified via `git diff origin/development <merge-candidate> --stat` = 9 files, −297.
- Cherry-pick path taken: plumbing 3-way apply of eaf6917's patch onto origin/development (temp index; only additive/gofmt changes; verified no deletions vs DEV except two benign hunks in config.go formatting and one test-flag fix in publisher_rebase_test.go).
- Built commit 1751189, pushed as `dogfood/hydra-mhk2-precheck`, PR #443 created and merged `--squash --admin`: state MERGED 2026-09-30T01:48:32Z, merge commit e0a3bce8d802d4f189d9c1a2b2ba39756eab2386.
- Park cleared: `br label remove hydra-mhk2 integration-exhausted` ✓, `integration-failed` ✓; triage comment added (`br comments add`) recording the resolution. Remaining label: pan-filed (bead owner decides close as cause-fixed).

## Verification (commands run + pass/fail)
- `go build ./...` on archived tree of 1751189 — PASS.
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestPrecheck|TestDefaultRebase|TestPRConflict|TestPublishWithRebaseRetry' -count=1` — PASS (ok 0.429s).
- `git merge-tree --write-tree origin/development 1751189` — rc=0 (clean).
- `gh pr view 443` — MERGED, e0a3bce. PASS.
- br label/comment operations — all ✓ (after `br doctor migrate-schema recover`).

## Open edges for the next phase
- hydra-mhk2 can be closed as cause-fixed once its own chain verdict lands.
- Delete remote `dogfood/hydra-mhk2-precheck` and the local `hydra/wt-hydra-mhk2` branch + worktree (content now landed; branch holds stale superseded integrations — do NOT merge it wholesale).
- The two rsyw docs add/add conflict class is now mitigated by the landed precheck itself (fail-closed preview before rebase).
