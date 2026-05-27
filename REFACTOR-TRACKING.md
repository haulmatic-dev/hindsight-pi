# Refactor Tracking: stable-streamline

## Branch
`refactor/stable-streamline` (from `fix/hindsight-consolidation`)

## Progress

### P0: Critical

- [x] **1. Decompose Overseer God Object** (partial — 20 methods extracted)
  - [x] Extract SpawnRateLimiter (5 methods)
  - [x] Extract RetryManager (7 methods)
  - [x] Extract ProjectScanner (2 methods)
  - [x] Extract BeadFileReader (2 methods)
  - [x] Extract ErrorLogger (2 methods)
  - [x] Extract BeadAnalyzer (2 methods)
  - [ ] Extract WorkerManager (deferred — high coupling)
  - [ ] Extract Scheduler (deferred — high coupling)
  - [ ] Extract ChainOrchestrator
  - [ ] Extract ResourceGovernor
  - [ ] Extract ValidationEngine
  - [x] Extract IntelligenceHub (file created, integration pending)

- [ ] **2. Slim Down Test Files**
  - [ ] Extract test fixtures to `testdata/`
  - [ ] Create `internal/testutil` mock factories
  - [ ] Use golden files for expected outputs
  - [ ] Split `overseer_integration_test.go`
  - [ ] Reduce `lsp_test.go`, `pi_config_test.go`

### P1: High Value

- [ ] **4. Validation as Service**
- [ ] **5. Intelligence Hub Facade** (file created)
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

## Commits So Far (18)

1. `a317e9c` refactor(cli): add unified CLI runner package
2. `ece1b1d` refactor(cleanup): remove dead code packages
3. `6c911a7` refactor(cleanup): remove dead consensus-facilitator code
4. `f855478` refactor(cleanup): remove duplicate ActivityMonitor
5. `88bae16` refactor(cleanup): remove unused AgentRegistry
6. `c6add83` refactor(overseer): extract SpawnRateLimiter
7. `ce30268` refactor(overseer): extract RetryManager
8. `ac163e1` docs: update refactor tracking with metrics
9. `34fda3e` refactor(overseer): add IntelligenceHub (not yet integrated)
10. `f0a84bb` refactor(overseer): extract ProjectScanner
11. `a0b9443` refactor(overseer): extract BeadFileReader
12. `9298f9f` docs: update refactor tracking with latest metrics
13. `latest` refactor(overseer): extract ErrorLogger
14. `latest` refactor(overseer): extract BeadAnalyzer

## Metrics

| Metric | Before | Current | Change |
|---|---|---|---|
| overseer.go lines | 3,976 | **3,503** | -473 |
| Overseer methods | 112 | **94** | -18 |
| Total source files | ~250 | ~250 | +0 (removed dead, added new) |
| Dead code removed | — | 1,336 lines | — |
| New packages | — | 7 | — |
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

## Next Steps

1. **Integrate IntelligenceHub** into Overseer (replaces initializeIntelligenceClients)
2. **Extract WorkerManager** (spawn/cleanup/completion) — needs careful testing
3. **Extract Scheduler** (getReadyBeads, getAllDescendants) — high coupling
4. **Flatten Pi Config structs** (7 nested types → 3)
5. **Extract test fixtures** from large test files
