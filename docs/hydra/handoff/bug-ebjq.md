# bug handoff (hydra-ebjq)

## What changed (files + one line each)
- internal/core/spawn_stale_claim_probe_test.go (new): 2 regression probes pinning the anti-wedge contract — transient spawn failure AFTER the spawning placeholder claims the slot leaves zero activeWorkers entries and the bead is immediately re-spawnable (no "worker already exists" block); a phantom claim with no tracker row is self-healed by the next Spawn attempt's duplicate check.
- CHANGELOG.md: Added entry under Unreleased.
- No production code change: the hypothesized defect is not present on this base (see verification).

## Verified findings (verify-first close, A1/A2/A3)
- A1 reproduction on this base: the hypothesized mechanism `isBeadBlockingSpawn` does not exist (`rg -n isBeadBlockingSpawn internal/` → 0 hits). The actual contract: overseer.spawnWorker runs the br-show (GetBead) preflights BEFORE any map write; WorkerSpawner.Spawn creates the {status:"spawning"} placeholder only after duplicate/registry/limit checks, and a deferred rollback deletes it on every failure path (worker_spawner.go:387-394, comment: "Any failure after this point must remove it; otherwise a {status:'spawning'} phantom blocks the bead forever (pool deadlock, 2026-08-24)").
- Probe test TestSpawn_TransientFailureLeavesNoStaleClaim simulates the incident class (fork/exec failure after placeholder claim, matching the br-show-exit-8 transient): 2 consecutive attempts both fail transiently, both leave 0 activeWorkers entries, neither returns "worker already exists" — no permanent silent skip.
- Probe test TestSpawn_StaleClaimWithoutTrackerRowSelfHeals pins the Charon-reconcile alternative the bead proposed: a stale entry with no tracker row is cleaned by the next Spawn attempt ("Cleaning up orphan worker" path), so even a hypothetically leaked claim self-heals instead of blocking forever.
- A3 no duplicate machinery: all four mechanisms the bead could ask for already exist and are tested — (1) placeholder rollback on failure (worker_spawner.go Spawn defer), (2) Pan repair `reap-placeholder` → ClearSpawningPlaceholder (service_starter.go:384), (3) digest-time spawn-failure purge `staleSpawnFailure` TTL 15m + live-registry supersession (service_starter.go:898), (4) Charon checkAndReapPoolDeadlock stale-placeholder reap (charon.go:1105, 10m threshold).
- graphify-out/graph.json present; graphify query used. hindsight not connected (skip rule).

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestSpawn_TransientFailureLeavesNoStaleClaim|TestSpawn_StaleClaimWithoutTrackerRowSelfHeals' -count=1` — PASS (2 tests).
- `go test ./internal/core/ -run 'TestSpawn|TestStaleSpawnFailure' -count=1` — PASS (35 tests, includes the 2 new probes).
- `gofmt -l internal/core/spawn_stale_claim_probe_test.go` — clean.
- `go vet ./internal/core/` — clean.

## Open edges for the next phase
- None for this wedge: the incident mechanism is covered by the rollback defer + self-healing duplicate check + reap/repairs.
- If a repeat of the 45-min silent skip is observed on a fresh incident, capture the deployed binary's build time vs origin/development tip first (recurring stale-binary pattern: hydra-5fbq, hydra-x60p) — the fix may be deployed-behind.
