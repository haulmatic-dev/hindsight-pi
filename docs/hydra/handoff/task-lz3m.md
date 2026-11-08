# green handoff (hydra-lz3m)

## What changed (files + one line each)
- internal/core/pan_health.go: `filePanBead` cap + same-title dedupe now count non-terminal (open + in_progress) pan-filed beads via `b.Status.IsTerminal()` skip instead of `b.Status != StatusOpen` — a filed bead claimed by a worker no longer frees its slot.
- internal/core/pan_filer_test.go: 2 regression tests — `TestFilePanBead_CapCountsInProgress` (cap holds while a filed bead is in_progress; terminal close frees the slot) and `TestFilePanBead_DuplicateInProgressTitleSkipped` (in_progress twin still hits `ErrFileBeadDuplicate`).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestFilePanBead' -count=1` — PASS (ok; includes new tests + pre-existing cap/label/dedupe tests).
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on touched files — clean.
- Commit 7469c86 verified via `git show --stat` (3 files, +57/−6).

## Open edges for the next phase
- Cap is package-level (`pan.MaxOpenFiledBeadsPerProject = 2`); the promised `shepherd.pan.max_filed_beads_per_project` config knob (hydra-xtfj handoffs) has no current reference in code — if reintroduced, wire it through filePanBead, not the pan package.
- Cap counting is per-ListBeads snapshot; concurrent filings in one tick could race past the cap (single Pan tick serializes in practice).
