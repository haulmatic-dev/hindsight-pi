# task handoff (hydra-0vc6)

## What changed (files + one line each)
- No code change in this worktree's own strand — the deliverable was the human merge of PR #78, executed via the cherry-pick + admin-merge pattern.
- docs/hydra/handoff/task-0vc6.md: this handoff.

## Verified outcome (PR #78)
Task: hydra-push-pending PUSH-PENDING because PR #78 (dogfood/publish-hydra → development) failed to merge and waited on human action.

- PR #78 tip b3f0360 was a POISONED commit: its diff vs parent 623be6a reverted landed code (deletes qmf7/wv2e digest fixes, empty_close_gate, credential_preflight, scheduler orphan reconcile, yz8z docs — 37 files, −1313). Wholesale merge rejected; GitHub mergeStateStatus DIRTY/CONFLICTING.
- Supersession audit vs origin/development: qmf7 grace (0a2a6c9/354bd81), heartbeat-fresh completed-row fix (0d4aeaa), wv2e content — all LANDED via PR #70. UNLANDED: s9sg per-project publish backoff + CREDENTIAL push-failure class (merge da88d2e), 12di docs (0162be5), yz8z docs (1527cd4), yusp docs (d73b0b5/b3f0360).
- Resolution: cherry-picked the unlanded strand onto origin/development as `dogfood/cherry-78` (078f7d4 s9sg code via `cherry-pick -m 1`, b53a2ac, 336e732, then c2db1ca extracting ONLY the yusp doc files from poisoned b3f0360 — the full commit cherry-pick would have reverted landed code).
- Verified before merge: `go build ./...` PASS; `go test ./internal/core/ -run 'TestPublishBackoff|TestClassifyPushFailure|TestResolvePublishPath|TestPublishRepo'` — 9 subtests PASS.
- Opened PR #81 (dogfood/cherry-78 → development), merged with `gh pr merge 81 --merge --admin` — VERIFIED MERGED: merge commit `d2f1f975ecfc9e6cad70be717d837f031b5c8cdc`, mergedAt 2026-10-02T03:56:32Z.
- PR #78 CLOSED with supersession note (gh pr close, state CLOSED verified).
- Remote scratch branch dogfood/cherry-78 deleted.

## Open edges for the next phase
- The daemon publish loop may still force-reset `dogfood/publish-hydra`; a future publish cycle should now succeed (backoff fix landed) or open a fresh PR. If hydra-push-pending stays PUSH-PENDING, unpark/re-push via operator API is the lever.
- Poisoned-tip class recurrence: `git show <tip> --stat` (mass deletions vs parent) before merging any dogfood/publish-hydra PR — this is the second occurrence (PR #394 lineage, hydra-s9sg lineage).
