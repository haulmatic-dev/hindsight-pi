# task handoff (hydra-1s1v)

## What changed (files + one line each)
- docs/hydra/handoff/task-1s1v.md: this handoff (integration action bead; no code change in this worktree — the strand landed via PR #23).
- CHANGELOG.md: Documentation entry recording the PR #23 merge (in the cherry-picked strand; see below).
- Cherry-picked and landed strand (via dogfood/hydra-4yuw → PR #23, merge d218837):
  - internal/core/publish_path.go (+ publish_path_test.go): new NOT_FOUND PublishPathClass — "repository not found" / "fatal: repository" push failures get their own class routed to PARK_LOCAL_LOUD, distinguishable from TRANSIENT in publish_path_classified events.
  - docs/discovery/publisher-wedge-triage-4yuw.md: full triage of the org-transfer flap (2,372 not-found push failures 2026-10-01, disk cause stale at 41%).
  - docs/hydra/handoff/task-4yuw.md + CHANGELOG entries for both commits.

## Verification (commands run + pass/fail)
- `git merge-tree --write-tree origin/development hydra/wt-hydra-4yuw` — rc=1 conflict + wholesale diff deletes newer landed code (epic_close_guard, spawn_preflight) → wholesale merge REJECTED. PASS (evidence).
- Ancestry check: e68a4a8 + 5c4baad NOT-LANDED on origin/development; fca0721 + 78351c0 LANDED. PASS.
- `git grep -i "not.found" origin/development -- internal/core/publish_path.go` — no hits → NOT_FOUND classifier not superseded upstream. PASS.
- Cherry-pick e68a4a8 + 5c4baad onto origin/development (branch dogfood/cherry-4yuw, tips 047e1a3 + 0f2f237) — both applied clean. PASS.
- `go build ./internal/core/ ./cmd/worker/` — PASS.
- `go test ./internal/core/ -run 'TestClassifyPushFailure|TestResolvePublishPath' -count=1` — PASS.
- `git push --force origin dogfood/cherry-4yuw:refs/heads/dogfood/hydra-4yuw` — forced update (remote branch held stale full history 5c4baad). PASS.
- `git merge-tree --write-tree origin/development origin/dogfood/hydra-4yuw` post-force — rc=0 clean (tree b2038bf). PASS.
- `gh pr merge 23 --merge --admin` — MERGED 2026-10-01T16:34:05Z, merge commit d218837. PASS.
- Post-merge: `git merge-base --is-ancestor e68a4a8 origin/development` + 5c4baad → both LANDED; origin/development tip d218837. PASS.
- Live store: `br label remove hydra-4yuw integration-exhausted|integration-failed` + triage comment — all ✓. PASS.

## Disposition (hydra-4yuw hard-park)
Root cause (org-transfer flap, canonical org decided, dev branches reconciled via PR #22) RESOLVED. Remaining unique content landed via PR #23. Hard-park labels cleared; scheduler retry expected to close via already-integrated/zero-diff path. Not a shared pipeline wedge — one-off repo/credential flap, already remediated.

## Open edges for the next phase
- Worktree hydra-1s1v stash@{0} holds superseded PR-already-exists work (landed upstream via PR #443) — safe to drop.
- Watch next 4yuw scheduler retry: if it re-fails instead of closing zero-diff, inspect close-path base resolution.
- Watch next publish tick for `publish_path_classified` events showing NOT_FOUND class on any future repo-access flap.
