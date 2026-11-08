# fix handoff (fix-lwoma, original hydra-7ty3 / hydra-8uw4 chain)

## What changed (files + one line each)
- internal/core/worker_completion_test.go: `setupWorkerCompletionTest` (helper, 14 call sites) now (1) sets `config.Intelligence.Graphify.Enabled = false` and (2) writes deterministic close-evidence files (`close-evidence.txt` into `repo`, `multiRoot/backend`, `multiRoot/frontend`) via a `writeCloseEvidence` closure before any close. Kills the flake and restores deterministic verified-close.
- CHANGELOG.md: Fixed entry under Unreleased.
- docs/hydra/handoff/fix-lwoma.md: this handoff.

## Root cause (two-layer, both verified)
1. **TempDir race**: `DefaultConfig()` enables Graphify (config.go) → Overseer constructs `graphifyIndexer` → successful `closeBead` calls `QueueRepoIndexing` → `Refresh` goroutine writes into the fixture repo concurrently with `t.TempDir()` RemoveAll cleanup → `TempDir RemoveAll cleanup: unlinkat /tmp/TestWorkerCompletion_ConcurrentCompletions<id>: directory not empty` (gate log `/opt/dev-tools/var/hydra/gates/hydra/hydra-lwoma/test.log` L4460; `FAIL` total at L4705).
2. **Tests depended on the race**: upstream the suite only passed because graphify's async `graphify update` dropped untracked files into the fixture repo, satisfying the verified-close evidence gate (`changedFilesWithBase`, scope_gate.go:206 — status-only when base=="", fixture repos have no origin so committed-evidence probe fails open; empty status → deterministic `empty-diff` reject, bead_closer.go:1390). Disabling Graphify alone flipped 6 tests to `verified-close: bead test-project-N rejected (empty-diff)` (proved via stash flip: WITH evidence-only fix → reject, WITHOUT → pass).

Fix addresses both layers in the test helper; production Graphify default untouched (smallest-change discipline).

## Verification (commands run + pass/fail)
- RED repro (pre-fix): `go test ./internal/core/ -run 'TestWorkerCompletion_ConcurrentCompletions' -count=5` — intermittent FAIL with the unlinkat TempDir error (exact temp-dir digits vary per run).
- `go test ./internal/core/ -run 'TestWorkerCompletion_ConcurrentCompletions' -count=5` (post-fix) — PASS `ok github.com/haulmatic-dev/hydra/internal/core 1.056s`.
- `go test ./internal/core/ -run 'TestWorkerCompletion' -count=1` — PASS `ok github.com/haulmatic-dev/hydra/internal/core 2.198s` (was 5 failures: SuccessfulCompletion L232, ProjectPathResolution ×3 subtests L388, CloseErrorHandling L444, DependencyErrorDetection L494, MultipleCloseAttempts L918 — all empty-diff rejects after graphify-off alone).
- Re-confirmed at close: same family run — PASS `ok ... 1.996s`.
- `gofmt -l internal/core/worker_completion_test.go` — clean.
- Note: `FAIL src/x/a.spec.ts` lines in the gate log are Prespawn fixture output, NOT a real failing suite (same disposition as feature-aizm handoff).

## Open edges for the next phase
- `intelligence_hub.go` QueueRepoIndexing goroutine has no test-visible lifecycle; production close paths writing into a worktree concurrently with cleanup remain theoretically race-prone but are out of scope here (production Graphify default untouched by design).
- 23 upstream gofmt-dirty files on origin/development remain out of scope (zero overlap with this branch's diff, verified in fix-8uw4 handoff).
