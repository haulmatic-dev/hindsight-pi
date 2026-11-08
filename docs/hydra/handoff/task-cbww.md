# refactor handoff (cbww)

## What changed (files + one line each)
- internal/core/tool_health_evidence.go: `ready()` helper unifies the probe-nil guard previously duplicated in `Start()` and `scan()`; `emitDegraded(project, memories)` extracts the `tool_degraded` emission; `intelSweepInterval` + new `evidenceActivityWindow` moved into one named const block. Behavior unchanged.
- internal/core/evidence_probes.go: activity-window const lifted from closure to the shared package-level `evidenceActivityWindow`.
- cmd/worker/intel_usage.go: `emitIntelUsageEvent` decomposed into `intelUsageRecord` (record builder, timestamp format preserved byte-for-byte) + `appendEventLine` (JSONL append with wrapped errors); error logging identical.
- CHANGELOG.md: Unreleased/Changed entry.

Note: GREEN implementation landed via cherry-pick of f9da350 (RED) + 179563a (GREEN) from hydra/wt-hydra-0qe7 — those commits were not in this worktree's base; refactor applies on top.

## Verification (commands run + pass/fail)
- BEFORE (post cherry-pick, pre-refactor): `go test ./internal/core/ -run 'TestToolHealthEvidence' -count=1` — PASS (8 subtests); `go test ./cmd/worker/` — PASS.
- AFTER: same commands — PASS. `go build -buildvcs=false ./...` — PASS (no output). `go vet ./internal/core/ ./cmd/worker/` — clean. `gofmt -l` on the three touched files — clean (pre-existing unformatted test files elsewhere untouched).
- No benchmarks exist for this functionality; no perf check run.

## Open edges for the next phase
- The intel_usage fields map (hindsight/cass/graphify_fallback) is defined twice — core.IntelUsageEvent and cmd/worker.intelUsageRecord — across package boundaries; unifying would require moving the record type to a shared package (left as-is, cross-package contract pinned by tests in both packages).
- hindsight_search was not connected at bead start (skip rule applied); graphify-out/graph.json absent (rg fallback).
