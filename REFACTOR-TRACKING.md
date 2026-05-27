# Refactor Tracking: stable-streamline

## Branch
`refactor/stable-streamline` (from `fix/hindsight-consolidation`)

## Progress

### P0: Critical

- [x] **1. Decompose Overseer God Object** (partial — 34 methods extracted)
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
  - [ ] Extract ChainOrchestrator
  - [ ] Extract ResourceGovernor
  - [ ] Extract ValidationEngine

- [ ] **2. Slim Down Test Files**
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

- [ ] **3. Config Consolidation**
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

- [ ] **10. Package Reorganization**

## Commits So Far (37)

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

## Metrics

| Metric | Before | Current | Change |
|---|---|---|---|
| overseer.go lines | 3,976 | **1,765** | -2,211 (-56%) |
| Overseer methods | 112 | **84** | -28 |
| Total source files | ~250 | ~250 | +0 (removed dead, added new) |
| Dead code removed | — | 1,336 lines | — |
| New packages | — | 20 | — |
| Tests | — | **542 passed** ✅ | — |

## New Packages

| Package | Purpose |
|---|---|
| `pkg/cli/runner.go` | Unified CLI command execution |
| `internal/core/spawn_rate_limiter.go` | Spawn rate limiting + backoff |
| `internal/core/retry_manager.go` | Bead retry + validation retry tracking |
| `internal/core/project_scanner.go` | Project discovery (config + auto) |
| `internal/core/bead_file_reader.go` | Bead file I/O (issues.jsonl) |
| `internal/core/intelligence_hub.go` | TLDR/GPTCache/Hindsight clients |
| `internal/core/error_logger.go` | Structured error logging |
| `internal/core/bead_analyzer.go` | Bead metadata analysis |
| `internal/core/dag_navigator.go` | Thread-safe DAG accessors |
| `internal/core/state_controller.go` | Runtime state (running, paused, workers) |
| `internal/core/config_manager.go` | Dynamic config changes |
| `internal/core/chain_registrar.go` | Chain registration from issues.jsonl |
| `internal/core/dag_builder.go` | Dependency graph construction |
| `internal/core/impact_analyzer.go` | TLDR-based impact analysis |
| `internal/core/worker_env_builder.go` | Worker environment variables |
| `internal/core/bead_discovery.go` | Ready bead finding from BR/BV |
| `internal/core/project_resolver.go` | Path resolution for beads |
| `internal/core/worker_lifecycle.go` | Worker process lifecycle |
| `internal/core/scheduler.go` | Main scheduling loop |
| `internal/core/service_starter.go` | Background service lifecycle |
| `internal/core/worker_spawner.go` | Worker process creation |

## Remaining Big Methods in Overseer

| Method | Lines | Notes |
|---|---|---|
| Start | ~50 | Main loop, now delegates to ServiceStarter + Scheduler |
| initializeMCP | ~49 | MCP-specific, could extract to MCPManager |
| initializeSettledStateService | ~41 | Could extract to SettledStateInitializer |
| initializeChainCompletionDetector | ~42 | Could extract to ChainCompletionInitializer |
| submitExecutionResultToBatchRetention | ~49 | MCP message handler |
| sendMCPWorkerCompletion | ~31 | MCP notification |
| runPiSetup | ~47 | Pi ecosystem setup |
| validateScope | ~31 | Scope validation |
| closeBead | ~39 | Bead closure + validation |
| spawnWorkersForNodes | ~39 | DAG-based spawning |
| getRetryEligibleBeads | ~40 | Retry bead filtering |

## Next Steps

1. **Extract MCPManager** (MCP methods cluster)
2. **Extract Initializer types** (settled-state, chain-completion)
3. **Flatten Pi Config structs** (32 config struct types)
4. **Extract test fixtures** from large test files
5. **PR to fix/hindsight-consolidation**
