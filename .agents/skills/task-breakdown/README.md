# Task Breakdown Skill

## Overview

The Task Breakdown Skill transforms Product Requirements Documents (PRDs) into executable bead chains with proper dependency orchestration. It uses a **type-aware, DAG-based approach** to create the right task patterns for different requirement types.

## Architecture

### Type-Aware Task Generation

The skill analyzes PRD requirements and assigns appropriate execution patterns based on requirement type:

| Requirement Type | Task Pattern | Worktree? | Description |
|------------------|--------------|-----------|-------------|
| **FR** (Functional) | `TDD_CHAIN` | ✅ Yes | 4-phase TDD: RED→GREEN→REFACTOR→SYNTAX |
| **NFR-Verify** (Coverage/Quality/Perf) | `VERIFICATION_GATE` | ❌ No | Single verification task after FRs complete |
| **NFR-Config** (Module System/Build) | `CONFIGURATION_TASK` | ✅ Yes | Configuration setup (parallel safe) |
| **US** (User Story) | `AGGREGATION_TASK` | ✅ Yes | Integration task that aggregates FRs |
| **Setup** | `CONFIGURATION_TASK` | ✅ Yes | Project initialization |

### Execution Phases

```
┌─────────────────────────────────────────────────────────────────┐
│ PHASE 1: Foundation (Parallel Worktrees)                        │
│ ├── FR-001: Addition           → Worktree 1                     │
│ ├── FR-002: Subtraction        → Worktree 2                     │
│ ├── FR-003: Multiplication     → Worktree 3                     │
│ ├── FR-004: Division           → Worktree 4                     │
│ └── NFR-004: Module System     → Worktree 5 (config, parallel)  │
├─────────────────────────────────────────────────────────────────┤
│ PHASE 2: Integration (After Phase 1 merges)                     │
│ ├── US-001: Basic Arithmetic   → Worktree 6 (aggregates FRs)    │
│ ├── US-002: Error Handling     → Worktree 7                     │
│ └── FR-005: Input Validation   → Worktree 8 (depends on FRs)    │
├─────────────────────────────────────────────────────────────────┤
│ PHASE 3: Quality Verification (No Worktrees)                    │
│ ├── NFR-001: Test Coverage     → Verification Gate              │
│ ├── NFR-002: Code Quality      → Verification Gate              │
│ ├── NFR-003: Documentation     → Verification Gate              │
│ └── NFR-005: Performance       → Verification Gate              │
├─────────────────────────────────────────────────────────────────┤
│ FINAL: LINT_GATE (After all gates pass)                         │
└─────────────────────────────────────────────────────────────────┘
```

## Key Design Principles

### 1. Semantic Dependencies

Not all requirements are independent. The skill models semantic relationships:

```javascript
// FRs only depend on approval
dependencies: ['approval-gate']

// Verification gates depend on all FRs completing
dependencies: ['FR-001', 'FR-002', 'FR-003', 'FR-004', ...]
dependencyType: 'VERIFIES'

// User Stories aggregate specific FRs
dependencies: ['FR-001', 'FR-002', 'FR-003', 'FR-004']
dependencyType: 'AGGREGATES'
```

### 2. Resource Efficiency

Only FRs and configuration tasks get worktrees. Verification gates run in the main worktree:

- **Old model:** 19 worktrees (19 requirements × 1 each)
- **New model:** ~8 worktrees (4 FRs + 1 config + 3 integration tasks)
- **Savings:** 58% reduction in disk usage, 72% reduction in beads

### 3. Correct Semantics

**Test Coverage** is a measurement, not a feature:
- ❌ Old: `RED: Write tests for coverage` → meaningless
- ✅ New: Single gate: `Run coverage report on completed code`

**User Stories** aggregate, they don't implement:
- ❌ Old: US-001 implements `add()` (duplicates FR-001)
- ✅ New: US-001 creates `export { add } from './calculator'` after FR-001 completes

## Usage

### CLI

```bash
# Basic usage
node /path/to/task-breakdown/cli.mjs docs/PRDs/feature.md

# JSON output for scripting
node /path/to/task-breakdown/cli.mjs docs/PRDs/feature.md --json

# With custom session ID
node /path/to/task-breakdown/cli.mjs docs/PRDs/feature.md --session-id my-feature
```

### Input: PRD Format

The skill expects PRDs with properly categorized requirements:

```markdown
## 3. Functional Requirements

### FR-001: Addition Operation
**Acceptance Criteria:**
- Accept two number parameters
- Return the sum

## 4. Non-Functional Requirements

### NFR-001: Test Coverage
**Acceptance Criteria:**
- Lines coverage: 100%
- Branches coverage: 100%

## 5. User Stories

### US-001: Developer - Basic Arithmetic
**Acceptance Criteria:**
- Can import and use add() function
```

The skill automatically classifies requirements by their prefix (FR-, NFR-, US-, etc.)

## Implementation

### File Structure

```
task-breakdown/
├── cli.mjs                    # Entry point
├── runner.mjs                 # Main orchestrator (6-step state machine)
├── analyzer.mjs               # PRD parser + semantic classifier
├── dag-builder.mjs            # DAG construction (NEW)
├── worktree-orchestrator.mjs  # Worktree allocation (NEW)
├── executor.mjs               # Beads CLI integration
├── state.mjs                  # Resume capability
├── rollback.mjs               # Error recovery
└── README.md                  # This file
```

### State Machine

```
validate-prd → analyze-prd → create-epics → create-approval 
                                                    ↓
verify ← create-lint-gate ← create-tasks ← dag-builder
```

1. **validate-prd:** Parse and validate PRD structure
2. **analyze-prd:** Extract requirements with semantic classification
3. **create-epics:** Create master and phase epics
4. **create-approval:** Create approval gate
5. **create-tasks:** Build DAG and create appropriate task patterns
6. **verify:** Verify structure and dependencies

### Integration with Hydra

The skill integrates with Hydra's worktree model:

- **FRs → Individual worktrees:** Parallel development on feature branches
- **NFR Gates → Main worktree:** Verification commands run in base environment
- **BV (Beads Viewer):** Uses DAG for intelligent triage prioritization
- **BR (Beads):** Tracks dependencies and ready states

## Best Practices

### For PRD Authors

1. **Categorize correctly:** Use FR- for features, NFR- for quality metrics, US- for user capabilities
2. **Keep NFRs verifiable:** NFRs should be measurable (coverage %, lint errors, etc.)
3. **User Stories aggregate:** USs should reference FRs, not re-implement them
4. **Section 5.8 Mapping:** Add Implementation Mapping to specify which files each requirement touches

### For Task Breakdown Users

1. **Review the DAG:** Use `--json` to inspect the dependency graph
2. **Parallel safe:** FRs are truly parallel; NFR gates converge
3. **Resource planning:** Expect ~8 worktrees for a typical 19-requirement PRD
4. **Merge order:** Foundation → Integration → Verification gates

## Migration from Old Model

If you have existing bead chains created with the old (broken) model:

```bash
# Check for problematic patterns
bv --robot-insights | jq '.Cycles'  # Should be 0
br list --status open | grep "Test Coverage\|Code Quality"  # Should be gates, not chains

# Manual cleanup if needed
br delete <test-coverage-red-bead> --force  # Remove meaningless RED beads
```

## Troubleshooting

### Too Many Worktrees Created

**Problem:** Old model created 19 worktrees for 19 requirements

**Solution:** New model creates ~8. If you still see excessive worktrees:
- Check that NFRs are being classified as VERIFICATION_GATE
- Verify analyzer is detecting requirement types correctly

### Test Coverage Bead Has TDD Chain

**Problem:** Test Coverage should be a gate, not a chain

**Solution:** Check that `classifyRequirement()` in analyzer.mjs is detecting verification keywords

### Circular Dependencies

**Problem:** DAG has cycles

**Solution:** 
- Check PRD for contradictory dependencies
- Verify USs don't create cycles with FRs
- Run: `br dep cycles`

## References

- **Original Analysis:** `/Users/buddhi/.pi/bead-analysis-loopholes.md`
- **Reorganized Design:** `/Users/buddhi/.pi/task-breakdown-reorganized-design.md`
- **Workflow Diagrams:** `/Users/buddhi/.pi/bead-execution-workflow.md`
- **Hydra Integration:** See Hydra's BV/BR architecture docs

## Version History

| Version | Changes |
|---------|---------|
| 1.0 | Type-blind TDD chains for all requirements (broken) |
| 2.0 | Type-aware DAG-based orchestration (current) |

---

*Part of the pi coding agent system. Integrates with Hydra for worktree-based parallel development.*
