# task handoff

## What changed (files + one line each)
- internal/core/publisher.go: `gh pr create` "already exists" fallback — `isPRAlreadyExists` detector + `resolveOrCreatePR` wrapper (resolve pre-existing open/merged PR via `resolveExistingPR`/`gh pr view <head>`, then merge); both `PublishViaPR` and `publishBranchViaPROnce` route through it. `defaultAutoPRGhMerge` tolerates already-merged race via `ghPRState` recheck; `ghPRView`/`ghPRState` injectable hooks.
- internal/core/publisher_existing_pr_test.go: 4 tests — existing-PR resolved and merged, no-PR keeps original error, CLOSED PR not resolvable, already-merged race verified as success.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (after hook-var fix).
- `go test ./internal/core/ -run 'TestDefaultAutoPRGhCreate_ResolveExistingPR|TestAutoPRGhMerge_AlreadyMergedRace' -count=1` — PASS (6 tests).
- `go test ./internal/core/ -run 'TestPublish|TestPRConflict|TestPublishBranchViaPR|TestMaybeOpenAutoPR|TestAutoPR|PrFirst|PRFirst' -count=1` — PASS (28 tests, no regressions).
- `gofmt -l` on changed files — clean.

## Root cause (verified from events)
- /opt/dev-tools/var/hydra/events/2026-09-29.jsonl: `ajantha-srv-telematics-f10l` — integration-failed twice in 3 minutes: (1) rebase onto origin/development hit CONFLICT in jest.config.ts + test/setup/env.ts ("rebase attempt 1 failed — human review needed"); (2) retry hit `gh pr create: exit status 1 ... a pull request for branch "dogfood/ajantha-srv-telematics-f10l" into branch "development" already exists: https://github.com/haulmatic-dev/srv-telematics/pull/330` — unhandled, looped, consecutive-failure cap → integration-exhausted hard-park. Same PR-already-exists pattern hard-parked 7 srv-telematics beads earlier (PRs #295/#297/#298/#300/#302). srv-smart-engine bead store additionally has schema mismatch (expected 19, found 11) requiring `br doctor migrate-schema` before mutation.

## Open edges for the next phase
- Rebase-conflict class (f10l attempt 1) still fail-closed human-review — FR-011 cap 2 is in place; these f10l conflicts are test-config files, likely need human resolution or branch abandonment per supersession rule.
- After deploy: existing-PR fallback should auto-unwedge the parked srv-telematics beads on next integration_retest; parks may then be cleared via repair:clear-hard-park.
- srv-smart-engine bead store needs `br doctor migrate-schema` (11 → 19) — infra, not code.
