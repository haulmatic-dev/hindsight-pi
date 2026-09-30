# implement handoff

## What changed (files + one line each)
- internal/core/integration_exhausted_sweep.go: new `IntegrationExhaustedSweeper` — periodic (1h cadence + once at startup) surfacing of stale integration-exhausted hard-parks: non-terminal bead carrying `integration-exhausted` with `UpdatedAt` age > threshold gets `needs-human` added (idempotent; already-flagged skipped) plus typed `integration_exhausted_stale` event. The hard-park itself is NEVER cleared (durable by design); in_progress and terminal beads skipped; fail-open; nil-safe; disabled when ttl <= 0 (scan() itself inert, Start no-op).
- internal/core/config.go: `overseer.integration_exhausted_sweep` duration knob, default 48h in DefaultConfig, 0 disables.
- internal/core/overseer.go: `integrationSweep` field, constructed in NewOverseer when knob > 0 (o.GetProjects + eventSink).
- internal/core/service_starter.go: Start/Stop wiring alongside escalationTTL scanner.
- internal/core/integration_exhausted_sweep_test.go: 3 tests — surfaces stale park (needs-human added), skips fresh/in_progress/terminal/already-flagged, disabled-at-0 (scan inert + Start no-op).
- hydra-config.yaml.example, docs/architecture/shepherd.md, CHANGELOG.md: operator docs + entry.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run 'TestIntegrationSweep|TestEscalationTTL' -count=1` — PASS (all, incl. 3 new + 4 existing escalation tests).
- `gofmt -w internal/core/config.go` — applied clean.
- Commit bf8124a on this branch, 8 files +276/−10 (verified via git show --stat).

## Open edges for the next phase
- Age proxy is UpdatedAt (same trade-off as EscalationTTL): any label touch resets the clock — conservative direction.
- `needs-human` label is shared vocabulary; clear-escalation repair already removes it — Pan may extend a recipe to handle surfaced integration parks (retry-quarantine or abandon-with-summary).
- integration_retest stage owns auto-retry of these beads (branch must still exist + merge clean); sweeper deliberately does NOT auto-clear or auto-retry.
