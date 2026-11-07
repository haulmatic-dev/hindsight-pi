# GREEN handoff — task-ytukk

Publish-path junk guard: fail-closed with a bounded, visible exit.

## What changed

No source changes this bead. Work already landed by prior strand (task-ai97h /
task-yevco), verified present at HEAD:

- `internal/core/publisher_junk.go` — `publishJunkImplemented = true` (line 50),
  real `defaultPublishJunkDiffProbe` (git diff branch vs `origin/<branch>`,
  `git log --name-only` fallback, classified via `junksig.IsJunkPath`,
  fail-open on git probe errors).
- `publishJunkGuard` — fail-closed: junk diff → push refused + one
  `publish_junk_blocked` event naming junk paths verbatim; per-project
  consecutive counter, ≥3 consecutive blocks (`publishJunkMaxBlocks`) →
  `publish-junk-escalated` reason, events stop after cap (bounded churn);
  clean/removal resets counter; failed probe fail-opens without touching state.
- Wired into `PublishRepo` step 2c before push (`internal/core/publisher.go`).

Disposition: **superseded (verify-first)**.

## Verification

```
go test ./internal/core/ -run 'TestPublisherJunk_' -count=1
  → ok  github.com/haulmatic-dev/hydra/internal/core  0.483s   (6/6 PASS)
go build ./...  → BUILD_OK
go vet ./...    → VET_OK
```

## Open edges

- Probe fail-open path has lower coverage (~72% on `defaultPublishJunkDiffProbe`);
  not AC-required.
- Counter is process-local; resets on daemon restart — accepted.
- Pre-existing committed gofmt defect in `internal/core/worktree_reconciler.go`
  (over-indented comment block); unrelated chain, recorded to CASS, not fixed here.

## HYDRA_VERDICT

```
HYDRA_VERDICT: {"done": true, "summary": "superseded (verify-first): junk guard landed by prior strand; tests/build/vet green; docs-only artifact is this handoff", "ac_met": ["AC1 fail-closed junk guard + publish_junk_blocked event", "AC2 escalation after publishJunkMaxBlocks=3", "AC3 clean branch pushes / counter reset", "AC4 fail-open on git probe errors", "bounded event churn"], "files_changed": ["docs/hydra/handoff/task-ytukk.md"], "verification": ["go test TestPublisherJunk_ 6/6 PASS", "BUILD_OK", "VET_OK"]}
```
