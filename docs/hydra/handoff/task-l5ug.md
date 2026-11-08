# bug handoff

## What changed (files + one line each)
- internal/core/worker_spawner.go: `isRegistryWorkerLive` now reconciles a non-terminal registry row whose heartbeat is stale beyond `deadRowGrace` (5m, mirrors Charon's `heartbeatHungAfter`) AND whose PID is dead/unset as `orphan-reaped` and allows the spawn — instead of failing every respawn with "worker already exists for bead" until Charon's hourly ghost reap. Fresh-heartbeat rows still block (PID-reuse / reporter.Stop-in-flight window); alive-PID rows still block (hang path is Charon's); reconciliation write error fails closed (row keeps blocking). New `processAlive` renamed `spawnProbeAlive` (collision with gate_artifact.go) — signal-0 PID probe.
- internal/registry/registry.go: new `MarkCompletedWithReason(beadID, exitCode, reason)` — terminal transition with explicit `err_message` provenance.
- internal/core/worker_spawner_dead_row_test.go (new): 4 tests — dead-PID stale row reconciled (terminal, exit=-1, err_message=orphan-reaped, not live); fresh-heartbeat dead-PID row still blocking; alive-PID stale row still blocking; closed-store lookup no crash.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run 'TestSpawn_DeadPID|TestSpawn_FreshHeartbeat|TestSpawn_AlivePID|TestSpawn_ReconcileFailure|TestSpawn_Parked|TestSpawn_StaleParked|TestSpawn_SpawningPlaceholder' -count=1` — PASS (7+).
- `go test ./internal/registry/ -count=1` — PASS.
- Full package via compiled binary: `go test -c -o /tmp/core.test ./internal/core/ && /tmp/core.test` — PASS, RC=0.

## Open edges for the next phase
- Reconciliation does not emit a typed event (log-only). If operators need it in the stream, add `spawn_row_reconciled` event at the same site.
- Parked rows are deliberately excluded from spawn-path reconciliation — their liveness is a scheduler decision (`parkedRespawnTimeout` path unchanged).
- The deterministic recover-spawn-cohort sweep (bead text) is a separate mechanism; this fix removes the underlying race so the cohort does not form.
