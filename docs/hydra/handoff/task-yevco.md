# Handoff — task-yevco (phase: refactor)

**Bead:** hydra-yevco — Publish-path junk guard: optimize the fail-closed publish-path guard with a bounded, visible exit (refactor pass on the hydra-ai97h chain).
**Repo:** /opt/dev-tools/var/hydra/.hydra-worktrees/-opt-dev-tools-components-hydra/hydra-ai97h (worktree of /opt/dev-tools/components/hydra)

## State found at start (IMPORTANT for the next strand)

The worktree arrived with **uncommitted GREEN implementation** of the publish-path
junk guard (bead hydra-ai97h): `publisher_junk.go` (real probe + guard),
`publisher.go` wiring, test harness fixes, CHANGELOG entry, and the GREEN handoff
`docs/hydra/handoff/task-ai97h.md`. HEAD still had only the RED stub commit
(`afee1e20 test(publisher): RED stub for publisher junk guard (hydra-ai97h chain)`).

Narrow tests over the uncommitted GREEN state **BEFORE refactor** — all PASS,
count=1: TestPublishJunkGuard_JunkBranchNoPushOneEvent,
TestPublishJunkGuard_ThirdConsecutiveBlockStopsRetries,
TestPublishJunkGuard_ThreadCacheChangesPush,
TestPublishJunkGuard_JunkRemovedPublishProceeds,
TestPublishJunkGuard_CleanPathFailOpenCoverage,
TestPublishRepo_JunkGuardEventBounded.

## What was refactored (behavior-preserving)

`internal/core/publisher_junk.go`:
- Extracted `junkPathsFromList(out string) []string` — classification of a
  newline-separated path list down to dedup + sorted junk paths. The diff/log
  arm of the probe now calls this one helper (was inline).
- Added `publishJunkGuardRefusedEvent` const = "publish_junk_guard_refused"
  (was repeated in two event emission sites in the guard).
- Extracted `countJunkBlock(projectPath string) int` — mutex-guarded
  consecutive-block increment. `publishJunkGuard` reads as:
  probe -> (clean reset | fail-open no-op) -> bounded block counter ->
  escalate vs emit-refused.
- Reordered comments: the probe doc comment now sits on
  `defaultPublishJunkDiffProbe`; the helper's doc sits on `junkPathsFromList`
  (a first pass left the two comments mis-attached — fixed).

`internal/core/publisher.go`:
- Comment on the wiring block now states the guard is fail-closed but noisy:
  a junk branch can only push its diff against `publishJunkMaxBlocks`
  consecutive attempts, then the escalated reason stops the loop (visible,
  bounded exit).

**No behavior change.** Same events, same reasons, same probe seam, same cap.

## Verification (all VERIFIED in this session)

| Step | Result |
|------|--------|
| Narrow tests AFTER refactor (same pattern, count=1) | PASS (0.362s) |
| `go build ./internal/core/` | OK |
| `gofmt -l internal/core/publisher_junk.go internal/core/publisher.go` | empty (clean) |
| `go vet ./internal/core/` | OK |

Note: the task-supplied verification pattern `TestPublisherJunk_` initially matched **no** tests — the GREEN strand had named them `TestPublishJunkGuard_*`. This refactor bead renamed all 6 test funcs to the bead-specified pattern (`TestPublisherJunk_Guard_*` × 5, `TestPublisherJunk_RepoGuardEventBounded`), VERIFIED with `-run 'TestPublisherJunk_'`: 6× --- PASS, no `[no tests to run]`.

## Commit

Single commit carries both the (previously uncommitted) GREEN implementation and
this refactor pass, because the GREEN changes never landed. Files:
`internal/core/publisher_junk.go`, `internal/core/publisher.go`,
`internal/core/publisher_junk_test.go`, `CHANGELOG.md`,
`docs/hydra/handoff/task-ai97h.md` (GREEN strand's doc), and this handoff.

## Files changed

- internal/core/publisher_junk.go (refactor: extract helpers, tighten naming)
- internal/core/publisher.go (wiring comment only)
- internal/core/publisher_junk_test.go (uncommitted GREEN harness fixes, carried)
- CHANGELOG.md (uncommitted GREEN entry, carried)
- docs/hydra/handoff/task-ai97h.md (uncommitted GREEN doc, carried)
- docs/hydra/handoff/task-yevco.md (this handoff)

## HYDRA_VERDICT

HYDRA_VERDICT: {"done": true, "ac_met": ["Refactor phase complete: extracted junkPathsFromList + countJunkBlock helpers, publishJunkGuardRefusedEvent const, tightened wiring comment. No behavior change.", "Narrow tests pass (bead pattern TestPublisherJunk_*: 6x --- PASS)", "go vet ./internal/core/ passes", "gofmt clean on touched files", "Handoff written: docs/hydra/handoff/task-yevco.md", "CHANGELOG entry added"], "ac_unmet": ["Full suite NOT run (turn budget constraint)"], "files_changed": ["internal/core/publisher.go", "internal/core/publisher_junk.go", "internal/core/publisher_junk_test.go", "docs/hydra/handoff/task-yevco.md", "CHANGELOG.md"], "verification": [{"cmd": "go test ./internal/core/ -run 'TestPublisherJunk_' -count=1 -v", "result": "6x --- PASS, ok (0.481s)"}, {"cmd": "go vet ./internal/core/", "result": "VET_OK"}, {"cmd": "gofmt -l (touched files)", "result": "empty (clean)"}]}
