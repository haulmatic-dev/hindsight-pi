# Refactor Tracking: stable-streamline

## Branch
`refactor/stable-streamline` (from `fix/hindsight-consolidation`)

## Progress

### P0: Critical

- [x] **1. Decompose Overseer God Object** (partial — 12 methods extracted)
  - [x] Extract SpawnRateLimiter (5 methods)
  - [x] Extract RetryManager (7 methods)
  - [ ] Extract WorkerManager
  - [ ] Extract Scheduler
  - [ ] Extract ChainOrchestrator
  - [ ] Extract ResourceGovernor
  - [ ] Extract ValidationEngine
  - [ ] Extract IntelligenceHub

- [ ] **2. Slim Down Test Files**
  - [ ] Extract test fixtures to `testdata/`
  - [ ] Create `internal/testutil` mock factories
  - [ ] Use golden files for expected outputs
  - [ ] Split `overseer_integration_test.go`
  - [ ] Reduce `lsp_test.go`, `pi_config_test.go`

### P1: High Value

- [ ] **4. Validation as Service**
- [ ] **5. Intelligence Hub Facade**
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

## Commits So Far

1. `a317e9c` refactor(cli): add unified CLI runner package
2. `ece1b1d` refactor(cleanup): remove dead code packages
3. `6c911a7` refactor(cleanup): remove dead consensus-facilitator code
4. `f855478` refactor(cleanup): remove duplicate ActivityMonitor
5. `88bae16` refactor(cleanup): remove unused AgentRegistry
6. `c6add83` refactor(overseer): extract SpawnRateLimiter
7. `ce30268` refactor(overseer): extract RetryManager

## Metrics

| Metric | Before | Current | Change |
|---|---|---|---|
| overseer.go lines | 3,976 | 3,739 | -237 |
| Overseer methods | 112 | 101 | -11 |
| Total source files | ~250 | ~245 | -5 |
| Dead code removed | — | 1,336 lines | — |
| New packages | — | 3 (cli, spawn_rate_limiter, retry_manager) | — |

## Next Steps

1. **Extract IntelligenceHub** from Overseer (~10 methods)
2. **Extract WorkerManager** from Overseer (~8 methods)
3. **Flatten Pi Config structs** (7 nested types → 3)
4. **Extract test fixtures** from large test files
