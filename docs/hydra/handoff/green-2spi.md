# green handoff (2spi)

## What changed (files + one line each)
- internal/core/graphify_path_guard.go (new): `project_path_missing` event (TTL 24h) + `pathExists`/`pathGuardNow`/`notePathMissing`/`checkPathExists`/`clearMissingPath` helpers.
- internal/core/graphify_indexer.go: Refresh stats abs path before claim — nonexistent path returns false, spawns no goroutine; reappearing path clears missing marker and refreshes. New fields `missingPaths` + `events`, `SetEventSink`.
- internal/core/overseer.go: indexer event sink wired from `o.eventSink`.
- internal/core/graphify_path_guard_test.go (new): 4 tests — nonexistent refused (no claim/no binary call), event exactly once per 24h TTL (TTL-expiry re-emit pinned), reappearing path refreshes + marker cleared, existing path refreshes.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestGraphifyPathGuard' -count=1` — PASS (4/4).
- `go build -buildvcs=false ./...` — PASS (rc=0).
- `go vet ./internal/core/` — PASS (clean).
- Coverage (same guarded run): pathExists 100%, pathGuardNow 100%, checkPathExists 100%, notePathMissing 87.5%, clearMissingPath 80%, Refresh 87.5% — ~93% on new guard code.

## Open edges for the next phase
- `notePathMissing` emits via central stream only (no repo mirror — path does not exist to mirror into).
- Events readback in tests globs `*.jsonl` under the sink dir (day-file naming).
