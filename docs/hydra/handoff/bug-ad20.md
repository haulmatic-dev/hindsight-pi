# bug handoff

## What changed (files + one line each)
- internal/core/spawn_preflight.go (new): fail-closed spawn preflight — `isNonExecutableBeadType` predicate, `spawnPreflightBead` (empty Type → `spawn_blocked_type_missing`; approval-gate/approval/epic/hold or hold label → `spawn_blocked_human_gate`), `spawnPreflightPublishIdentity` (non-daemon owner without PAT → `spawn_blocked_identity_missing`, auto-resume; transient errors fail open), all returning `SpawnSuppressedError` (no retry burned, rechecked next tick) and emitting repo-mirrored events via `emitSpawnPreflightBlocked`.
- internal/core/overseer.go: `spawnWorker` (single choke point for Tick, SpawnWorkersForNodes/BV-plan, and all retry/respawn paths) now (1) refreshes empty Type/Project from the store `GetBead` read it already performs immediately before spawn, then (2) runs both preflights before `prespawnCloseIfGreen`/`Spawn`.
- internal/core/worker_spawner.go: `SpawnSuppressedError` gains `Reason` field; `Error()` renders reason when Status empty (non-terminal preflight blocks share the suppression contract).
- internal/core/multi_user_e2e_test.go: fixture writes alice's `.hydra/.github-token` — the new identity preflight (by design fail-closed) would otherwise block her spawns.
- CHANGELOG.md, docs/architecture/shepherd.md: operator-facing entry + 3 new typed events documented.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run 'TestSpawnPreflight|TestSpawnWorker_|TestMultiUser_EndToEnd|TestResurrection|TestScheduler|TestShouldSpawnWorker' -count=1` — PASS.
- `go test ./internal/core/ -count=1` (full package) — PASS (63.6s).
- 9 new tests: empty-Type blocked/allowed matrix, non-executable types (case-insensitive), hold-label, store-truth refresh (in-memory empty Type refreshed → gate blocked), empty-everywhere fail-closed, identity preflight missing-PAT/PAT-resolves/daemon-inert.

## Open edges for the next phase
- DAG worker-exit-as-completion: `spawnWorker` no longer spawns gates, but if a gate somehow reaches completion handling, the DAG releasing dependents while br stays open is a second seam — closeBead already fails loud (`br show failed`), which is the backstop.
- Identity preflight resolves via `getUserContextForProject` (multi-user discovery contexts); single-user daemon configs with `user:` token but no PAT file are daemon-owned and unaffected.
- `agent_worker.go` (agent-connector path) got no preflight — it is not in the daemon scheduler spawn funnel (used by agent sessions); add if it ever routes scheduled beads.
