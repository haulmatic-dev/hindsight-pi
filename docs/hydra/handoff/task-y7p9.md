# green handoff

## What changed (files + one line each)
- internal/core/publisher.go: `prAlreadyExistsError` detector + `defaultAutoPRGhResolveURL` (gh pr view <head> --json url) + `openOrResolvePR` — `gh pr create` "already exists" now falls back to resolving and merging the existing open PR in both PublishViaPR and publishBranchViaPROnce (srv-telematics wedge root cause: unhandled exit 1 looped retries → integration-exhausted hard-park).
- internal/core/publisher_test.go: autoPRFakeExec gains ghResolve/resolveURL/resolveErr; 3 new tests (fallback resolves+merges, resolve failure fails closed, non-already-exists passthrough).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` + `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestPublishViaPR_AlreadyExists|TestPublishViaPR_NonAlreadyExists|TestPublishViaPR_Propagates|TestPublishBranchViaPR' -count=1` — PASS (10 tests).

## Open edges for the next phase
- Live: parked srv-telematics beads (38ot/f10l/burr/0po4/qj74 + 242b group) can be retried after this lands — publish path is now idempotent on the open-PR race.
- maybeOpenAutoPR already resolves existing PRs; only the PublishViaPR/PublishBranchViaPR paths needed the fix.
