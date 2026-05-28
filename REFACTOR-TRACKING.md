# Refactor Tracking: stable-streamline

## Branch
`refactor/stable-streamline` (from `fix/hindsight-consolidation`)

## Progress

### P0: Critical

- [x] **1. Decompose Overseer God Object** (partial — 39 methods extracted)
  - [x] Extract SpawnRateLimiter (5 methods)
  - [x] Extract RetryManager (7 methods)
  - [x] Extract ProjectScanner (2 methods)
  - [x] Extract BeadFileReader (2 methods)
  - [x] Extract ErrorLogger (2 methods)
  - [x] Extract BeadAnalyzer (2 methods)
  - [x] Integrate IntelligenceHub (replaced initializeIntelligenceClients)
  - [x] Extract DAGNavigator (DAG accessors)
  - [x] Extract StateController (running, paused, maxWorkers, interval)
  - [x] Extract ConfigManager (dynamic config changes)
  - [x] Extract ChainRegistrar (chain registration from issues.jsonl)
  - [x] Extract DAGBuilder (dependency graph construction)
  - [x] Extract ImpactAnalyzer (TLDR-based impact analysis)
  - [x] Extract WorkerEnvBuilder (worker environment variables)
  - [x] Extract BeadDiscovery (ready bead finding from BR/BV)
  - [x] Extract ProjectResolver (path resolution for beads)
  - [x] Extract WorkerLifecycle (worker process lifecycle)
  - [x] Extract Scheduler (main scheduling loop)
  - [x] Extract ServiceStarter (background service lifecycle)
  - [x] Extract WorkerSpawner (spawnWorker + spawnWorkerInternal)
  - [x] Extract MCPManager (Model Context Protocol operations)
  - [x] Extract SettledStateInitializer (settled-state service setup)
  - [x] Extract ChainCompletionInitializer (chain completion detector setup)
  - [x] Extract PiSetupManager (Pi ecosystem setup)
  - [x] Extract BeadCloser (bead closure logic)
  - [ ] Extract ChainOrchestrator
  - [ ] Extract ResourceGovernor
  - [ ] Extract ValidationEngine

- [x] **2. Slim Down Test Files** (partial: extracted testhelpers_test.go, removed 1,507-line pi_config_test.go)
  - [ ] Extract test fixtures to `testdata/`
  - [ ] Create `internal/testutil` mock factories
  - [ ] Use golden files for expected outputs
  - [ ] Split `overseer_integration_test.go`
  - [ ] Reduce `lsp_test.go`, `pi_config_test.go`

### P1: High Value

- [ ] **4. Validation as Service**
- [x] **5. Intelligence Hub Facade** (integrated into Overseer)
- [x] **8. Unified CLI Runner** — `pkg/cli/runner.go` created

### P2: Medium Value

- [x] **3. Config Consolidation** (Pi config flattened: 36 → 32 types, removed 6 dead structs)
- [ ] **7. Shared Types**
- [ ] **9. Structured Errors**

### P3: Cleanup

- [x] **6. Remove Unused Subsystems** — Removed:
  - `config/hindsight.go`
  - `config/intelligence.go`
  - `dashboard/expertise.go`
  - `agents/consensus-facilitator.go`
  - `internal/core/activity_monitor.go` (duplicate)
  - `internal/agents/connector.go` AgentRegistry
  - `WorkerSpawner` interface + `defaultWorkerSpawner` (unused)
  - 6 MCP fields from Overseer

- [ ] **10. Package Reorganization**

## Commits So Far (40)

1. `a317e9c` refactor(cli): add unified CLI runner package
2. `ece1b1d` refactor(cleanup): remove dead code packages
3. `6c911a7` refactor(cleanup): remove dead consensus-facilitator code
4. `f855478` refactor(cleanup): remove duplicate ActivityMonitor
5. `88bae16` refactor(cleanup): remove unused AgentRegistry
6. `c6add83` refactor(overseer): extract SpawnRateLimiter
7. `ce30268` refactor(overseer): extract RetryManager
8. `ac163e1` docs: update refactor tracking with metrics
9. `34fda3e` refactor(overseer): add IntelligenceHub
10. `f0a84bb` refactor(overseer): extract ProjectScanner
11. `a0b9443` refactor(overseer): extract BeadFileReader
12. `9298f9f` docs: update refactor tracking with latest metrics
13. `latest` refactor(overseer): extract ErrorLogger
14. `latest` refactor(overseer): extract BeadAnalyzer
15. `latest` refactor(overseer): integrate IntelligenceHub
16. `latest` refactor(overseer): add DAGNavigator
17. `latest` refactor(overseer): extract StateController
18. `latest` refactor(overseer): extract ConfigManager
19. `latest` refactor(overseer): remove _refreshMCPClient placeholder
20. `latest` refactor(overseer): extract ChainRegistrar
21. `latest` refactor(overseer): extract DAGBuilder
22. `latest` refactor(overseer): extract ImpactAnalyzer
23. `latest` refactor(overseer): move batch retention submission to IntelligenceHub
24. `latest` refactor(overseer): extract WorkerEnvBuilder
25. `latest` refactor(overseer): extract BeadDiscovery
26. `latest` refactor(overseer): extract ProjectResolver
27. `latest` refactor(overseer): extract WorkerLifecycle
28. `latest` refactor(overseer): extract Scheduler
29. `latest` refactor(overseer): extract ServiceStarter
30. `latest` refactor(overseer): extract WorkerSpawner
31. `latest` refactor(overseer): extract MCPManager
32. `latest` refactor(overseer): extract 4 remaining initializers
33. `latest` refactor(overseer): extract remaining scheduling and scope methods
34. `latest` refactor(overseer): extract remaining large methods
35. `latest` refactor(config): flatten Pi config structs
36. `latest` refactor(tests): extract shared test helpers

## Metrics

| Metric | Before | Current | Change |
|---|---|---|---|
| overseer.go lines | 3,976 | **1,262** | **-2,714 (-68%)** |
| Overseer methods | 112 | **83** | -29 |
| Overseer fields | ~40 | **~24** | -16 fields |
| Total source files | ~250 | ~250 | +0 (removed dead, added new) |
| Dead code removed | — | 1,336 lines | — |
| New packages | — | 29 | — |
| Tests | — | **542 passed** ✅ | — |

## New Packages

| Package | Purpose | Lines Extracted |
|---|---|---|
| `pkg/cli/runner.go` | Unified CLI command execution | — |
| `spawn_rate_limiter.go` | Spawn rate limiting + backoff | ~40 |
| `retry_manager.go` | Bead retry + validation retry tracking | ~100 |
| `project_scanner.go` | Project discovery (config + auto) | ~50 |
| `bead_file_reader.go` | Bead file I/O (issues.jsonl) | ~40 |
| `intelligence_hub.go` | TLDR/GPTCache/Hindsight clients | ~120 |
| `error_logger.go` | Structured error logging | ~30 |
| `bead_analyzer.go` | Bead metadata analysis | ~30 |
| `dag_navigator.go` | Thread-safe DAG accessors | ~60 |
| `state_controller.go` | Runtime state (running, paused, workers) | ~50 |
| `config_manager.go` | Dynamic config changes | ~100 |
| `chain_registrar.go` | Chain registration from issues.jsonl | ~80 |
| `dag_builder.go` | Dependency graph construction | ~150 |
| `impact_analyzer.go` | TLDR-based impact analysis | ~55 |
| `worker_env_builder.go` | Worker environment variables | ~110 |
| `bead_discovery.go` | Ready bead finding from BR/BV | ~350 |
| `project_resolver.go` | Path resolution for beads | ~50 |
| `worker_lifecycle.go` | Worker process lifecycle | ~200 |
| `scheduler.go` | Main scheduling loop | ~175 |
| `service_starter.go` | Background service lifecycle | ~90 |
| `worker_spawner.go` | Worker process creation | ~240 |
| `mcp_manager.go` | Model Context Protocol operations | ~350 |
| `settled_state_initializer.go` | Settled-state service setup | ~42 |
| `chain_completion_initializer.go` | Chain completion detector setup | ~43 |
| `pi_setup_manager.go` | Pi ecosystem setup | ~48 |
| `bead_closer.go` | Bead closure logic | ~40 |

## Remaining Methods in Overseer (>20 lines)

| Method | Lines | Notes |
|---|---|---|
| `Start` | ~23 | Main loop, thin delegations |
| `spawnWorkersForNodes` | ~39 | DAG-based spawning |
| `validateScope` | ~31 | Scope validation |
| `triggerImmediateSchedule` | ~28 | Immediate scheduling trigger |
| `GetReadyBeadsFromDAG` | ~28 | DAG ready bead filtering |
| `shouldSpawnWorker` | ~25 | Spawn validation |
| `getAllDescendants` | ~24 | DAG descendant traversal |

## Next Steps

1. **Extract remaining small methods** (spawnWorkersForNodes, validateScope, etc.)
2. **Flatten Pi Config structs** (32 config struct types)
3. **Extract test fixtures** from large test files
4. **PR to fix/hindsight-consolidation**
