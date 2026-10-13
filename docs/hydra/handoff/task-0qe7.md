# green handoff (0qe7)

## What changed (files + one line each)
- internal/core/tool_health_evidence.go: real `ToolHealthEvidenceScanner` (probe-injected evidence sweep: worker activity + zero hindsight memories → one `tool_degraded` event with `probe:"evidence"` per project; zero activity → no events; probe errors fail-open logged) + `IntelUsageEvent(hindsight, cass, graphifyFallback)` building the `intel_usage` record; hourly Start/Stop loop like the other health sweepers.
- internal/core/evidence_probes.go (new): Overseer probes — `recentWorkerActivityProbe` (registry rows with fresh <24h heartbeat, nil-store fail-open to no-activity) and `hindsightMemoriesProbe` (HindsightClient Search count; nil/disabled client → 0,nil; query error propagates for fail-open).
- internal/core/overseer.go: `evidenceSweep` field + construction in NewOverseer when intelligence hub present (probes wired).
- internal/core/service_starter.go: evidenceSweep Start/Stop alongside integrationSweep.
- cmd/worker/intel_usage.go (new): `emitIntelUsageEvent` — worker writes the `intel_usage` JSONL record to `$HYDRA_HOME/events/YYYY-MM-DD.jsonl` (daemon EventSink shape), fail-open; `graphifyFallbackFor` checks for `graphify-out/graph.json`.
- cmd/worker/main.go: intel_usage emitted after prompt build / before prompt write to the agent (attempt-level), hindsight = memories retrieved, cass = CASS bullets non-empty, graphify_fallback = no graph file.
- Tests: internal/core/tool_health_evidence_test.go (cherry-picked RED, e9783c8 — 5 tests), internal/core/evidence_probes_test.go (5), cmd/worker/intel_usage_test.go (3).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestToolHealthEvidence' -count=1 -v` — PASS (6 passed).
- `go test ./internal/core/ -run 'TestEvidenceProbes' -count=1` — PASS.
- `go test ./cmd/worker/ -run 'TestEmitIntelUsage|TestGraphifyFallback' -count=1 -v` — PASS (3).
- `go build ./...` — PASS. `go vet ./internal/core/ ./cmd/worker/` — clean. gofmt clean on touched files.
- Coverage (new code): scan 91.7%, IntelUsageEvent 100%, NewToolHealthEvidenceScanner 100%, probes tested via evidence_probes_test.go, emitIntelUsageEvent 63.2%, graphifyFallbackFor 100%.

## Open edges for the next phase
- intel_usage is emitted per attempt after prompt build (hindsight field reflects actual recall). Zero-memory signal relies on the hindsight probe's Search, not per-execution retained memories — if "hindsight_memories" is meant as retained records per project, swap the probe to the retention store.
- emitIntelUsageEvent duplicates the Event JSONL shape inline (worker cannot import a daemon EventSink); if core.Event grows fields, keep the two shapes in sync.
- evidence_probes activity window hardcoded 24h (no config knob).
