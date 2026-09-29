# green handoff

## What changed (files + one line each)
- agents/context-processor.go: DefaultContextProcessorConfig MaxQueryLatency 500ms → 15s (500ms cancelled every daemon-side recall; measured recall p50 ~4.6s).
- internal/hindsight/client.go: new `WithBudget` option + `Budget` field on SearchRequest (recall API schema field, "low"/"medium"/"high").
- cmd/worker/main.go: queryHindsightForContext now creates the hindsight client with WithBudget("low") to bound candidates server-side for daemon pre-queries.
- agents/context-processor_test.go: TestDefaultContextProcessorConfig asserts 15s.
- CHANGELOG.md: operator-facing entry under Unreleased/Fixed.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS (BUILD_OK).
- `go test ./agents/ -count=1` — PASS (ok).
- `go test ./internal/hindsight/ -count=1` — PASS (ok).
- `go test ./cmd/worker/ -count=1` — PASS (ok).

## Open edges for the next phase
- Live end-to-end: spawn a worker on a bead in a worked area and confirm `✅ Retrieved N relevant memories from Hindsight` in the worker log (not the ℹ️ empty line). Not verifiable from this worktree.
- Concurrent recall during startup (fix option 2) not implemented — timeout raise + low budget reduce wall-time impact; revisit if 15s stalls prompt build.
