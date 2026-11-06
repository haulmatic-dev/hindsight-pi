# Handoff — task-ai97h (phase: green)

Bead: hydra-ai97h — Publish-path junk guard (GREEN phase).

## Summary

Implemented the fail-closed publish-path junk guard in `internal/core` so the
RED contract tests in `internal/core/publisher_junk_test.go` pass without
`PUBLISH_JUNK_ENFORCE=1`.

## Changes

- `internal/core/publisher_junk.go` (GREEN flip of the RED stub):
  - `publishJunkImplemented = true`.
  - `defaultPublishJunkDiffProbe`: real git diff probe — committed diff of the
    working branch vs `origin/<branch>` (`git diff --name-only origin/<b>...<b>`);
    falls back to `git log --name-only` when no remote-tracking ref exists so a
    never-pushed junk branch is still caught. Output classified by
    `junksig.IsJunkPath`, deduplicated + sorted. Any git failure (detached HEAD,
    broken repo) fails **open** — a broken probe never blocks a healthy publish.
  - `publishJunkGuard(projectPath)`: fail-closed guard. Junk diff → publish
    refused, one typed `publish_junk_blocked` event naming the offending paths
    verbatim. Per-project consecutive-block counter (mutex-guarded map): after
    `publishJunkMaxBlocks` (3) consecutive blocks the reason escalates to
    `publish-junk-escalated`, events stop (bounded churn), publish still
    refused. Clean branch or removal of junk resets the counter (fresh budget).
  - `resetPublishJunkBlocks`: per-project counter reset helper.
- `internal/core/publisher.go`: wired `publishJunkGuard` into `PublishRepo`
  before the push attempt — a blocked guard returns `(false, reason)` with no
  push attempted, mirroring the existing `publish_push_skipped_merged` flow.
- `internal/core/publisher_junk_test.go` (two RED-harness bugs fixed; these
  lines never executed in RED because the tests were require-gated skips):
  - `publishJunkRemoteCommits`: an empty bare remote (unborn HEAD, exit 128
    `fatal: your current branch 'master' does not have any commits yet`) is a
    legitimate "nothing ever pushed" state — exactly what AC1 expects after a
    blocked publish; it now returns nil instead of fatally failing the test.
  - `TestPublishJunkGuard_ThirdConsecutiveBlockStopsRetries`: the
    `project-other` assertion contradicted its own goal (its probe seam
    returns junk for every path, so the guard correctly refuses it with a
    blocked=true). The pin's intent — "other projects are unaffected by this
    project's counter" — is now asserted as the other project's reason being
    the plain (fresh-budget) `publish-junk`, never this project's escalated
    state.

## Verification (all commands run this session)

- `go test ./internal/core/ -run 'TestPublishJunkGuard_|TestPublishRepo_JunkGuardEventBounded' -count=1 -v`
  → 6/6 PASS, 0 failures, no `[no tests to run]`. All ACs pinned:
  - AC1 junk branch: no push, remote stays empty, one blocked event naming the
    junk path verbatim (`.jest-cache/jx`).
  - AC2 counter: 3 plain blocks with the plain reason, 4th call returns
    `publish-junk-escalated` and stays blocked; other projects get a fresh
    budget.
  - AC3 thread-cache.service.ts-only branch pushes normally, zero blocked
    events, commit lands on the remote.
  - AC4 junk removed → publish proceeds, counter resets (next junk is a plain
    block again).
  - Probe failure fails open; clean project never blocked.
  - Event churn bounded at exactly `publishJunkMaxBlocks` events.
- `go build ./...` → BUILD_OK.
- `go vet ./...` → VET_OK.
- Coverage (`go tool cover -func`): `defaultPublishJunkDiffProbe` 72.0% (the
  no-remote-tracking-ref fallback branch is untested — fail-open path, low
  risk), `publishJunkGuard` 100.0%, `resetPublishJunkBlocks` 100.0%.

## Notes for the next agent

- The guard lives entirely behind `publishJunkDiffProbe` (seam) +
  `publishJunkGuard` (entry) — wire any future publish/clean path (e.g. the
  close-path publish helpers) through the same entrypoint rather than
  re-implementing probe/classify logic.
- The counter is process-local state, same lifetime as the other publisher
  globals (`publishEvents`); it does not survive a daemon restart, which is
  acceptable: the guard re-blocks on the first junk diff after restart and
  re-escalates after 3 more consecutive blocks.
- `junksig` itself (`internal/junksig`) already has its GREEN gate flipped.

HYDRA_VERDICT:
status: GREEN
tests: PASS (6/6 targeted, count=1)
build: OK
vet: OK
coverage: guard 100%, reset 100%, probe 72%(fallback branch fail-open untested)
