# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-movi.md: triage of the five-bead shared integration-exhausted wedge — causes (stale daemon binary pre PR-reuse, br WAL poison, disk 99%) all fixed; per-bead dispositions verified.
- CHANGELOG.md: Unreleased entry recording the triage.
- Live-store actions (verified via br output): srv-telematics burr — removed `integration-exhausted` + `integration-failed` labels (✓×2) + triage comment (✓); hydra store — closed hydra-rsyw and hydra-6zwl not_planned with abandon-with-summary reasons (✓×2), `br doctor migrate-schema recover` run on both stores.

## Verification (commands run + pass/fail)
- `git merge origin/development` in worktree — clean; `go build ./...` — PASS.
- `go test ./internal/core/ -run 'TestPRAlreadyExists|TestResolveOrCreatePR|TestPublishBranchViaPR' -count=1` — PASS (1 test).
- `git merge-tree --write-tree origin/development origin/dogfood/ajantha-srv-telematics-burr` — rc=0 CLEAN → park clear safe (branch docs-only).
- `git merge-tree --write-tree origin/development origin/dogfood/hydra-rsyw` — CONFLICT (publisher.go, publisher_test.go, cache-cap-guard.sh add/add) → rsyw branch superseded by landed `prAlreadyExistsError`; no rebase.
- `git show origin/development:internal/core/publisher.go | grep -c isPRAlreadyExists` — 0 (landed name `prAlreadyExistsError`); PR #389 API: closed/merged.
- hydra-xxq5 already closed (WAL-poison root cause recorded); hydra-242b already closed (ng18 triage); otae carries no integration-exhausted label (failed-closure fix bead, no park).

## Open edges for the next phase
- br WAL private-recovery flake ("engine admission recovery changed protected database component \"-wal\"") recurs under daemon concurrency; needed explicit `br doctor migrate-schema recover` between mutations. Operator-level fix still open.
- If burr loops integration failure after the fair retry, disposition is abandon-with-summary, not re-park.
- Tier B agentic conflict resolution remains unimplemented.
