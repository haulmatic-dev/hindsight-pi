# green handoff

## What changed (files + one line each)
- internal/degrade/degrade.go: new package — process-global attempt/success counter registry for fail-open subsystems (FR-032). Tumbling window (DefaultWindow 24h, `HYDRA_DEGRADE_WINDOW` override); Degraded = >= MinAttemptsForDegraded (3) attempts && 0 successes inside the current window; Status.String() renders "name 0/N". Reset() for test isolation.
- agents/context-processor.go: QueryHindsight / QueryHindsightByID / QueryHindsightWithQuery record attempt before Search and success on nil error under subsystem "hindsight-recall" (cache hits not counted — counters sit after the cache check).
- cmd/worker/retain.go: recordRetainAttempt/recordRetainSuccess helpers under "hindsight-retain"; wired at both Retain call sites in cmd/worker/main.go (async + sync retention paths).
- internal/core/pan_health.go: store scans record "pan-store-scan" attempt/success around ListBeads (per-target); collectHealthDigest appends degraded snapshot fragments to digest.Degradations after collectDiskWatermark.
- internal/pan/health.go: HealthDigest.Degradations []string field; Text() renders `host: degradations=[...] — fail-open subsystem at 0% success; check its backing service` only when non-empty (stable-digest rule preserved).
- Tests: internal/degrade/degrade_test.go (8), internal/pan/health_degrade_test.go, internal/core/pan_health_degrade_test.go (digest snapshot, render, recovery clears), agents/context_processor_degrade_test.go (unreachable-endpoint client → attempts accrue), cmd/worker/retain_degrade_test.go.
- CHANGELOG.md: Added entry under Unreleased. docs/operations/host-setup.md: `HYDRA_DEGRADE_WINDOW` knob row.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/degrade/ ./agents/ ./cmd/worker/ ./internal/core/ ./internal/pan/` — PASS.
- `go test ./internal/degrade/ ./internal/pan/ ./agents/ ./cmd/worker/ -count=1` — PASS all four packages.
- `go test ./internal/core/ -count=1` — PASS (1152 tests, 55.6s).

## Open edges for the next phase
- Counters are process-local: worker-process subsystems (hindsight-recall, hindsight-retain) count in the worker and do NOT reach the daemon Pan digest; only daemon-side counters (pan-store-scan) surface today. Cross-process reporting (e.g. worker → daemon telemetry channel) is future work.
- Tumbling window: a success inside the current window masks a later 0%-success streak until the window rolls (up to 24h). If faster detection is wanted, add a trailing fail-streak rule.
