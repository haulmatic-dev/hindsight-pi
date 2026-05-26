# Self-Healing Validation Loops

## Feature Name

self-healing-validation-loops

## Overview

Hydra currently detects when a chain of beads completes but does not verify that the chain's goal was actually achieved. Two gaps exist:

1. **Chain-level TDD validation**: After REFACTOR completes, there is no check that the RED and GREEN beads produced sufficient tests and correct implementation. Broken tests or shallow implementations can slip through.

2. **End-of-chain PRD validation**: When a chain completes, there is no validation that the leaf implementations satisfy the root PRD/requirements bead. Phantom completions are possible.

This feature adds self-healing validation loops at both levels: validation runs, and on failure, fix beads are created automatically with retry limits.

## 1. Functional Requirements

### FR-001: CreateBead Method for Beads Client

**Type:** fr
**Priority:** P0

**Target Files:**
- `internal/beads/client.go`
- `internal/beads/client_test.go`
- `pkg/types/bead.go`

**Test Command:**
```
go test ./internal/beads/... -v -run TestCreateBead
```

**Build Command:**
```
go build ./...
```

**Reads From:**
- `br create` CLI command output format

**Exports:**
- `func (c *Client) CreateBead(projectPath string, req CreateBeadRequest) (types.BeadID, error)`

**Acceptance Criteria:**
- `CreateBead` accepts `CreateBeadRequest` with Title, Description, Type, Priority, Labels, and Dependencies
- `CreateBead` executes `br create` with `--json` flag and parses the resulting bead ID
- On success, returns the parsed `types.BeadID`
- On CLI error, returns wrapped error with stdout/stderr
- `CreateBeadRequest` struct is defined in `pkg/types/bead.go`
- Unit tests cover: success path, CLI error, parse error

**Depends On:** —

---

### FR-002: Validation Retry Tracking Infrastructure

**Type:** fr
**Priority:** P0

**Target Files:**
- `internal/core/overseer.go`
- `internal/core/config.go`
- `hydra.yaml.example`

**Test Command:**
```
go test ./internal/core/... -v -run TestValidationRetry
```

**Build Command:**
```
go build ./...
```

**Reads From:**
- Existing `beadRetryInfo` struct and `retries` map in overseer

**Exports:**
- `func (o *Overseer) getValidationRetryCount(beadID string) int`
- `func (o *Overseer) incrementValidationRetry(beadID string)`
- `func (o *Overseer) clearValidationRetry(beadID string)`

**Acceptance Criteria:**
- `validationRetries` map added to `Overseer` with `sync.RWMutex`
- `ValidationConfig` struct added to overseer config with `MaxRounds` field (default 3)
- `getValidationRetryCount` returns 0 for unknown beads
- `incrementValidationRetry` increments count atomically
- `clearValidationRetry` removes entry from map
- Config YAML example includes `validation.max_rounds: 3`
- Unit tests cover: get/increment/clear operations and max rounds config

**Depends On:** FR-001

---

### FR-003: TDD Chain Validation — Validator Agent and Bead Type

**Type:** fr
**Priority:** P1

**Target Files:**
- `internal/validation/tdd_validator.go`
- `internal/validation/tdd_validator_test.go`
- `pkg/types/bead.go`

**Test Command:**
```
go test ./internal/validation/... -v -run TestTDDValidator
```

**Build Command:**
```
go build ./...
```

**Reads From:**
- Bead metadata for `target_files`, test files
- PRD acceptance criteria from root bead

**Exports:**
- `func NewTDDValidator() *TDDValidator`
- `func (v *TDDValidator) ValidateTDDChain(redBead, greenBead, refactorBead types.Bead, projectPath string) (*TDDValidationResult, error)`

**Acceptance Criteria:**
- `TDDValidator` struct defined with configurable checks
- `ValidateTDDChain` performs three checks:
  1. **RED check**: Tests exist, compile, and have meaningful assertions (not just `assert true`)
  2. **GREEN check**: Implementation code exists and matches test expectations
  3. **Coverage check**: Tests cover acceptance criteria from the root bead description
- Returns `TDDValidationResult` with:
  - `Passed bool`
  - `RedPassed bool`
  - `GreenPassed bool`
  - `Gaps []TDDGap` (each gap has Type: "red" or "green", Description, Severity)
- Static checker validates test file existence and compilation
- Agent verifier (optional, when enabled) validates test sufficiency against acceptance criteria
- Unit tests mock file system and test compilation

**Depends On:** FR-002

---

### FR-004: TDD Chain Validation — Fix Bead Creation and Retry Logic

**Type:** fr
**Priority:** P1

**Target Files:**
- `internal/core/overseer.go`
- `internal/core/overseer_chain_validation.go` (new)

**Test Command:**
```
go test ./internal/core/... -v -run TestTDDValidationFlow
```

**Build Command:**
```
go build ./...
```

**Reads From:**
- `TDDValidator.ValidateTDDChain` result
- `beads.Client.CreateBead`
- `Overseer.validationRetries`

**Consumes:**
- `TDDValidationResult` from FR-003

**Acceptance Criteria:**
- New `RunTDDValidation` method on `Overseer`:
  - Called when a REFACTOR bead is about to close (after worker completes)
  - Runs `TDDValidator.ValidateTDDChain` for the REFACTOR bead's chain
  - If passed: proceeds to close REFACTOR normally
  - If failed and retry count < max_rounds:
    - Creates fix-red bead (if RED gap) or fix-green bead (if GREEN gap) using `CreateBead`
    - Fix bead depends on the REFACTOR bead
    - Creates `tdd-validate-{round}` bead that depends on the fix bead
    - Closes the original REFACTOR bead
    - Increments validation retry count
  - If failed and retry count >= max_rounds:
    - Logs warning: "TDD validation exhausted retries, proceeding to SYNTAX"
    - Closes REFACTOR normally (allows SYNTAX to run)
- Fix bead titles: `Fix: {original title} (validation round {n})`
- Fix bead descriptions include the gap description from validation
- `RunTDDValidation` is idempotent — calling twice for same bead is a no-op
- Unit tests cover: pass, fail with retry, fail exhausted retries, idempotency

**Depends On:** FR-003

---

### FR-005: End-of-Chain PRD Validation — Validator Agent and Bead Type

**Type:** fr
**Priority:** P1

**Target Files:**
- `internal/validation/prd_validator.go`
- `internal/validation/prd_validator_test.go`
- `pkg/types/bead.go`

**Test Command:**
```
go test ./internal/validation/... -v -run TestPRDValidator
```

**Build Command:**
```
go build ./...
```

**Reads From:**
- Root bead (PRD/requirements bead) description and acceptance criteria
- All leaf implementation beads' target files
- Chain info from `ChainCompletionDetector`

**Exports:**
- `func NewPRDValidator() *PRDValidator`
- `func (v *PRDValidator) ValidateChainAgainstPRD(chain *ChainInfo, projectPath string) (*PRDValidationResult, error)`

**Acceptance Criteria:**
- `PRDValidator` struct defined with configurable checks
- `ValidateChainAgainstPRD` performs:
  1. Extract acceptance criteria from root bead description
  2. Collect all target files from leaf implementation beads
  3. Static check: all target files exist and are non-empty
  4. Agent verification: compare implementation against acceptance criteria (when enabled)
- Returns `PRDValidationResult` with:
  - `Passed bool`
  - `Gaps []PRDGap` (each gap has Title, Description, RequiredFiles, Severity)
  - `RootBeadID types.BeadID`
  - `CheckedFiles []string`
- Unit tests mock chain info, file system, and agent verification

**Depends On:** FR-002

---

### FR-006: End-of-Chain PRD Validation — Fix Chain Creation and Loop

**Type:** fr
**Priority:** P1

**Target Files:**
- `internal/core/overseer.go`
- `internal/core/overseer_chain_validation.go`
- `internal/core/chain_completion_detector.go`

**Test Command:**
```
go test ./internal/core/... -v -run TestPRDValidationFlow
```

**Build Command:**
```
go build ./...
```

**Reads From:**
- `PRDValidator.ValidateChainAgainstPRD` result
- `beads.Client.CreateBead`
- `ChainCompletionDetector` callback

**Consumes:**
- `PRDValidationResult` from FR-005
- `ChainCompletedEvent` from `ChainCompletionDetector`

**Acceptance Criteria:**
- `ChainCompletionDetector` callback extended to trigger PRD validation:
  - When `ChainStatusCompleted` fires, call `RunPRDValidation`
- `RunPRDValidation` on `Overseer`:
  - Runs `PRDValidator.ValidateChainAgainstPRD`
  - If passed: logs success, chain fully done
  - If failed and retry count < max_rounds:
    - Creates fix beads for each gap using `CreateBead`
    - If multiple gaps: creates approval-gate bead that depends on all fix beads
    - Creates `prd-validate-{round}` bead that depends on fix beads (or approval gate)
    - Fix beads have type `fix`, title from gap title, description from gap description
    - Increments validation retry count
  - If failed and retry count >= max_rounds:
    - Logs warning: "PRD validation exhausted retries, chain marked complete despite gaps"
    - Does NOT reopen any beads
- Approval gate is auto-closed when all its dependent fix beads close (no human required)
- New PRD-validate bead runs in next scheduling cycle, re-triggering validation
- Unit tests cover: pass, fail with retry, fail exhausted retries, multi-gap fix chain

**Depends On:** FR-005

---

### FR-007: Integration of TDD Validation into Worker Completion Flow

**Type:** fr
**Priority:** P1

**Target Files:**
- `internal/core/overseer.go`
- `cmd/worker/main.go`

**Test Command:**
```
go test ./internal/core/... -v -run TestWorkerCompletionWithTDDValidation
```

**Build Command:**
```
go build ./...
```

**Reads From:**
- Worker completion notification via MCP
- `RunTDDValidation` from FR-004

**Acceptance Criteria:**
- When worker for REFACTOR bead completes, overseer calls `RunTDDValidation` before closing the bead
- If validation creates fix beads, worker completion is acknowledged but original bead stays in "pending validation" state
- Fix beads are picked up by next scheduling tick
- No duplicate workers spawned for original bead while fix beads are in progress
- Integration test simulates full flow: REFACTOR complete → TDD validation fail → fix bead created → fix complete → TDD validation pass → SYNTAX runs

**Depends On:** FR-004

---

## 2. User Stories

### US-001: Developer Runs TDD Chain with Auto-Fix

**Type:** us-light
**Priority:** P1

**Aggregates:**
- FR-003: TDD Chain Validation — Validator Agent and Bead Type
- FR-004: TDD Chain Validation — Fix Bead Creation and Retry Logic
- FR-007: Integration of TDD Validation into Worker Completion Flow

**Acceptance Criteria:**
- A TDD chain (RED → GREEN → REFACTOR) runs automatically
- After REFACTOR, validation checks if tests and code are correct
- If validation finds gaps, fix beads are created automatically
- Fix beads run with the same agent configuration as original beads
- After fixes pass validation, SYNTAX bead runs
- If validation fails 3 times, SYNTAX runs anyway (no infinite loops)

---

### US-002: Developer Runs Full Chain with PRD Validation

**Type:** us-light
**Priority:** P1

**Aggregates:**
- FR-005: End-of-Chain PRD Validation — Validator Agent and Bead Type
- FR-006: End-of-Chain PRD Validation — Fix Chain Creation and Loop

**Acceptance Criteria:**
- A full chain completes (all beads closed)
- End-of-chain validation checks if implementation matches the PRD
- If gaps found, fix beads are created automatically
- Fix beads form a sub-chain with proper dependencies
- After fixes complete, re-validation runs automatically
- If validation fails 3 times, chain is marked complete with a warning

---

## 3. Non-Functional Requirements

### NFR-001: Config Validation

**Type:** nfr-config
**Priority:** P0

**Target Files:**
- `internal/core/config.go`
- `hydra.yaml.example`

**Test Command:**
```
go test ./internal/core/... -v -run TestValidationConfig
```

**Build Command:**
```
go build ./...
```

**Acceptance Criteria:**
- Config struct includes `Validation` section with:
  - `Enabled bool` (default true)
  - `TDDValidationEnabled bool` (default true)
  - `PRDValidationEnabled bool` (default true)
  - `MaxRounds int` (default 3)
  - `AgentVerificationEnabled bool` (default true)
- `hydra.yaml.example` updated with new validation config section
- Config tests validate default values and custom values

**Verifies:**
- FR-002: Validation Retry Tracking Infrastructure

---

### NFR-002: Unit Test Coverage

**Type:** nfr-verification
**Priority:** P1

**Acceptance Criteria:**
- All new code has >80% test coverage
- Mock implementations exist for `beads.Client` and `validation.Validator`
- Table-driven tests for all validation result permutations
- Race condition tests for concurrent validation retry access

**Verifies:**
- All FRs

---

### NFR-SYS: System Integration Verification

**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Full project builds with `go build ./...` and exits 0
- Full test suite runs with `go test ./...` and exits 0
- No empty files or placeholder comments remain in target files
- Existing integration tests still pass
- Hydra starts successfully with new config

**Verifies:**
- All FRs and USs in this PRD

---

## 4. Technical Considerations

### Architecture

**Pattern:** Event-driven validation hooks
- TDD validation hooks into worker completion (before bead close)
- PRD validation hooks into chain completion callback
- Both use bead creation for fixes (no reopening — state machine stays monotonic)

**Why not reopen beads?**
- Reopening breaks overseer's `completed` map and `ChainCompletionDetector`
- Creates race conditions in DAG and worker scheduling
- Fix beads with dependencies are cleaner — they extend the chain naturally

### Technology
- Go 1.23+
- Existing `br create` CLI for bead creation
- Existing validation framework extended with TDD and PRD validators

### Project Structure
```
internal/
  beads/
    client.go          # +CreateBead
    client_test.go     # +CreateBead tests
  validation/
    tdd_validator.go         # new
    tdd_validator_test.go    # new
    prd_validator.go         # new
    prd_validator_test.go    # new
  core/
    overseer.go              # +validation retry tracking, +hooks
    overseer_chain_validation.go  # new: RunTDDValidation, RunPRDValidation
    config.go                # +ValidationConfig
```

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/beads/client.go`, `pkg/types/bead.go` | fr | — | 1 of 7 |
| FR-002 | `internal/core/overseer.go`, `internal/core/config.go` | fr | FR-001 | 2 of 7 |
| FR-003 | `internal/validation/tdd_validator.go` | fr | FR-002 | 3 of 7 |
| FR-004 | `internal/core/overseer_chain_validation.go` | fr | FR-003 | 4 of 7 |
| FR-005 | `internal/validation/prd_validator.go` | fr | FR-002 | 3 of 7 |
| FR-006 | `internal/core/overseer_chain_validation.go`, `internal/core/chain_completion_detector.go` | fr | FR-005 | 5 of 7 |
| FR-007 | `internal/core/overseer.go`, `cmd/worker/main.go` | fr | FR-004 | 6 of 7 |
| US-001 | — | us-light | FR-003, FR-004, FR-007 | — |
| US-002 | — | us-light | FR-005, FR-006 | — |
| NFR-001 | `internal/core/config.go`, `hydra.yaml.example` | nfr-config | — | — |
| NFR-002 | `internal/validation/*_test.go`, `internal/core/*_test.go` | nfr-verification | All FRs | — |
| NFR-SYS | — | nfr-verification | All FRs + USs | — |

### Edge Cases

- **Empty chain**: Chain with single bead — skip TDD validation (no RED/GREEN), still do PRD validation
- **No target files**: Validation gap reports missing files but doesn't crash
- **Concurrent validation**: Two workers complete simultaneously — retry map handles with mutex
- **Circular dependency from fix beads**: Fix bead depends on original, new validation depends on fix — not circular because original is already closed
- **Approval gate timeout**: If approval gate never closes (bug), chain hangs — mitigated by not using human approval gates (auto-close)

### Constraints
- Cannot reopen closed beads — must create new beads
- Must use existing `br create` CLI — no direct DB writes
- Must respect `max_workers` — fix beads are scheduled like any other beads
- Retry counts must persist across overseer restarts (use bead metadata for persistence)

---

## 5. MoSCoW

**Must Have:**
- FR-001: CreateBead method
- FR-002: Validation retry tracking
- FR-003: TDD validator
- FR-005: PRD validator
- FR-007: Worker completion integration for TDD validation

**Should Have:**
- FR-004: TDD fix bead creation
- FR-006: PRD fix chain creation

**Could Have:**
- Agent-based verification (vs static checks only)
- Email/Slack notification when validation fails exhausted retries

**Won't Have:**
- Reopening closed beads
- Human-in-the-loop approval gates (all automated)
- PRD validation for non-implementation chains (e.g., documentation-only chains)

---

## 6. Research Insights

### Codebase Findings
- `beads.Client` currently has `GetBead`, `ListBeads`, `CloseBead`, `UpdateBeadStatus` — no `CreateBead`
- `validation.Validator` exists but only checks end-of-chain beads by type (not actual chain position)
- `ChainCompletionDetector` fires callback on chain completion but only extracts patterns
- `beadRetryInfo` tracks worker retry state but not validation retry state
- `br create --json` exists and outputs the created bead

### Historical Patterns
- Previous fix: `CloseBead` now verifies closure status after `br close` call
- Previous fix: Config parsing supports both array and object formats for projects
- Pattern: New features added as optional (config `Enabled` flags) to avoid breaking existing behavior

### Project Context
- Hydra runs as daemon, polling for ready beads
- Workers are spawned as separate processes via `cmd/worker`
- MCP protocol used for worker-overseer communication
- Config file: `hydra.yaml`

---

## 7. Validation Checklist

- [x] Every requirement has `**Type:**` with a valid value
- [x] Every requirement has `**Priority:**` with P0, P1, or P2
- [x] Every requirement has `**Target Files:**` with specific paths
- [x] Every `nfr-config` requirement has `**Test Command:**` and `**Build Command:**`
- [x] All `nfr-verification` requirements have `**Verifies:**` sections
- [x] A System Integration Verification bead (NFR-SYS) exists
- [x] No two parallel FRs share a `Target Files` entry (FR-004 and FR-006 share `overseer_chain_validation.go` but are sequential)
- [x] All chained same-file FRs appear in Implementation Map
- [x] Zero subjective ACs
- [x] No circular dependencies
