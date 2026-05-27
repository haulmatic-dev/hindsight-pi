# Refactor Tracking: stable-streamline

## Branch
`refactor/stable-streamline` (from `fix/hindsight-consolidation`)

## Progress

### P0: Critical

- [x] **1. Decompose Overseer God Object** (partial — 24 methods extracted)
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
  - [ ] Extract WorkerManager (deferred — high coupling)
  - [ ] Extract Scheduler (deferred — high coupling)
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

- [ ] **10. Package Reorganization**

## Commits So Far (24)

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

## Metrics

| Metric | Before | Current | Change |
|---|---|---|---|
| overseer.go lines | 3,976 | **3,285** | -691 |
| Overseer methods | 112 | **92** | -20 |
| Total source files | ~250 | ~250 | +0 (removed dead, added new) |
| Dead code removed | — | 1,336 lines | — |
| New packages | — | 11 | — |
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

## Next Steps

1. **Extract WorkerManager** (spawn/cleanup/completion) — biggest remaining cluster
2. **Extract Scheduler** (getReadyBeads, getAllDescendants) — high coupling
3. **Migrate refresh methods** to use IntelligenceHub (remove old fields)
4. **Flatten Pi Config structs** (7 nested types → 3)
5. **Extract test fixtures** from large test files
