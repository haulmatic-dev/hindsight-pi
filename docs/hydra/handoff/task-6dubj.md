# red handoff

## What changed (files + one line each)
- internal/core/publisher_junk.go (new): RED-phase inert stub — constants `publishJunkReason` ("publish-junk"), `publishJunkEscalatedReason` ("publish-junk-escalated"), `publishJunkBlockedEvent` ("publish_junk_blocked"), `publishJunkMaxBlocks` (3); seam `publishJunkDiffProbe`/`defaultPublishJunkDiffProbe` (inert, nil); inert `publishJunkGuard` (always allow) + `resetPublishJunkBlocks`; `publishJunkImplemented=false`.
- internal/core/publisher_junk_test.go (new): 5 contract tests + unconditional fail-open wiring/coverage test — AC1 junk-diff branch → no push attempted (bare remote stays empty) + exactly one `publish_junk_blocked` naming the path verbatim; AC2 third consecutive block → escalated reason, guard still refuses, counter per-project; AC3 thread-cache.service.ts-only branch pushes via the real production probe; AC4 junk removal → publish allowed + counter reset (next block plain reason again); PublishRepo-level event churn bounded by publishJunkMaxBlocks.
- CHANGELOG.md: Added entry under Unreleased.

## Contract GREEN must implement
- Real `defaultPublishJunkDiffProbe(projectPath)`: committed diff of the working branch vs origin/<branch> (git probe, fail-open on error) classified by `junksig.IsJunkPath`.
- Real `publishJunkGuard`: per-project consecutive-block counter (Overseer field beside `publishBackoff` acceptable); blocked → reason `publish-junk`, ≥3 consecutive → `publish-junk-escalated`; probe error fails open.
- Wiring: `PublishRepo` calls `publishJunkGuard` after the up-to-date/already-integrated checks and BEFORE `publish_push_attempted`; blocked → skip push, emit one `publish_junk_blocked` (path in Detail), return (false, reason); stop emitting events once cap reached; successful/clean publish calls `resetPublishJunkBlocks`.

## Verification (commands run + pass/fail)
- `PUBLISH_JUNK_ENFORCE=1 go test ./internal/core/ -run 'TestPublishJunkGuard|TestPublishRepo_JunkGuard' -count=1` — FAIL, exit 1 for the RIGHT reason: 5× `RED: publish-path junk guard contract not yet implemented` (all 5 contract cases).
- `go test ./internal/core/ -run 'TestPublishJunkGuard|TestPublishRepo_JunkGuard' -count=1` — PASS (ok; contract tests SKIP loudly pre-GREEN, fail-open coverage passes), exit 0, no `[no tests to run]`.
- `go vet ./internal/core/` — clean (VET_OK). `gofmt -l` on both new files — clean.

## Open edges for the next phase
- overseer.go not touched this phase (minimal diff); GREEN adds the counter/service-store wiring there and the PublishRepo guard call site.
- AC3 depends on GREEN's default probe computing the real git diff in the clone fixture (origin/<branch> exists via bare remote).
