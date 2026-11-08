# Discovery handoff

## What changed (files + one line each)
- internal/core/pan_mirror_health.go — added `mirrorWriteProbe` seam (FR-4); `BrUpdater` state remains as corrected in e210b189, routing healing through `o.beadsClient.UpdateBeadStatus`.
- internal/core/pan_mirror_health_test.go — added 6 FR-4 contract RED tests pinning CreateBead sink usage, exactly-once ledger, fail-open event emission.
- CHANGELOG.md — Added entry for the FR-4 contract discovery.
- docs/hydra/handoff/task-qgvr.md — This handoff.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS (exit 0).
- `go vet ./internal/core/` — PASS (no output).
- `go test ./internal/mirroraudit/ -count=1` — PASS ("PASS: 1 passed").
- `go run ./tools/beads-mirror-audit --store /home/ajantha/projects/srv-jobfile/.beads` — PASS ("mirror clean (49 beads)", exit 0).
- `git status --short` — clean tree.

## Open edges for the next phase
- The lint-gate failures reported in the task description (`o.hydraHome`, `o.config.MirrorHealth`, `o.emitEvent`, `types` references at lines 132-239) are stale — the file is only 105 lines and `go vet` is clean.
- FR-4 GREEN implementation remains to be landed by the next phase (currently RED contract tests).
