# Refactor Tracking: stable-streamline

## Branch
`refactor/stable-streamline` (from `fix/hindsight-consolidation`)

## Progress

### P0: Critical

- [ ] **1. Decompose Overseer God Object**
  - [ ] Extract WorkerManager
  - [ ] Extract Scheduler
  - [ ] Extract ChainOrchestrator
  - [ ] Extract ResourceGovernor
  - [ ] Extract ValidationEngine
  - [ ] Extract IntelligenceHub
  - [ ] Slim down Overseer to coordinator

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

## Metrics

| Metric | Before | Current | Change |
|---|---|---|---|
| Total source files | ~250 | ~245 | -5 |
| Dead code removed | — | 1,086 lines | — |
| CLI runner unified | — | 189 lines | +1 pkg |

## Next Steps

1. Extract test fixtures from large test files
2. Flatten config structs (CollectiveUnconscious, Pi configs)
3. Decompose Overseer (RetryManager, WorkerManager first)
