# Product Requirements Document: Beads Client and Dependency Resolver

## 1. Executive Summary

This PRD defines the implementation requirements for completing the Hydra orchestrator's **Beads Client** and **Dependency Resolver (DAG)** components. These are Phase 1 deliverables that enable proper task scheduling with dependency tracking.

**Business Value:**
- Enable parallel execution of independent tasks
- Prevent downstream tasks from starting before dependencies complete
- Detect and report circular dependencies
- Support complex workflow patterns (fan-out, fan-in)

**Scope:**
- Complete beads client with JSON parsing
- Implement DAG-based dependency resolution
- Integrate with existing overseer polling loop
- Add comprehensive testing

## 2. User Stories

### US-001: Parse Full Bead Details
**As a** task scheduler  
**I want** to parse complete bead information from the `br` CLI  
**So that** I can understand task requirements, dependencies, and metadata

**Acceptance Criteria:**
- Parse bead ID, title, description, type from `br show` JSON output
- Extract dependency list with IDs and status
- Handle both single-repo and multi-repo projects
- Support all bead types (task, bug, feature, etc.)

### US-002: Build Dependency Graph
**As a** workflow orchestrator  
**I want** to build a DAG from bead dependencies  
**So that** I can determine execution order and identify ready tasks

**Acceptance Criteria:**
- Build graph from all beads and their dependencies
- Support up to 1000 beads per project
- Calculate in-degree for each bead
- Track which beads are blocked vs ready

### US-003: Detect Circular Dependencies
**As a** system operator  
**I want** to detect circular dependencies in the workflow  
**So that** I can prevent infinite loops and report configuration errors

**Acceptance Criteria:**
- Detect cycles during DAG construction
- Report the exact cycle path (e.g., A → B → C → A)
- Log error and skip affected beads
- Continue processing other valid beads

### US-004: Parallel Task Execution
**As a** developer  
**I want** independent tasks to execute in parallel  
**So that** overall workflow completion time is minimized

**Acceptance Criteria:**
- Execute beads with in-degree 0 simultaneously
- Respect max worker limit (default 6)
- Honor priority ordering when selecting from ready pool
- Track worker status via API

### US-005: Dynamic Unblocking
**As a** task scheduler  
**I want** dependent tasks to become ready when dependencies complete  
**So that** the workflow progresses automatically without manual intervention

**Acceptance Criteria:**
- Reduce in-degree when dependency completes
- Trigger immediate scheduling check (don't wait for poll interval)
- Handle partial failures (retry failed beads independently)
- Update DAG state atomically

## 3. Functional Requirements

### FR-001a: FR-001: Complete Beads Client JSON Parsing (Part 1/4)

Acceptance Criteria:
- Use `--json` flag for all commands (`br ready`, `br show`, `br list`)
- Define Go structs matching JSON schema from `br schema`
- Parse fields: id, title, description, status, priority, issue_type, labels, dependencies

### FR-001b: FR-001: Complete Beads Client JSON Parsing (Part 2/4)

Acceptance Criteria:
- Handle time fields (created_at, updated_at) with proper timezone handling
- Support dependency arrays with full dependency objects
- `GetReadyBeads()` parses JSON output correctly

### FR-001c: FR-001: Complete Beads Client JSON Parsing (Part 3/4)

Acceptance Criteria:
- `GetBeadDetails()` returns full bead with dependencies
- `ListBeads()` supports pagination (if applicable)
- All bead types are supported

### FR-001d: FR-001: Complete Beads Client JSON Parsing (Part 4/4)

Acceptance Criteria:
- Error handling includes CLI error messages
### FR-002a: FR-002: DAG Data Structure (Part 1/6)

Acceptance Criteria:
- Create `internal/core/dag.go` with DAG struct
- Use adjacency list representation for O(1) edge lookups
- Support concurrent reads (RWMutex for thread safety)

### FR-002b: FR-002: DAG Data Structure (Part 2/6)

Acceptance Criteria:
- Store bead metadata alongside graph structure
- Track execution state (pending, running, completed, failed)
- `AddNode(bead)` - Add bead to graph

### FR-002c: FR-002: DAG Data Structure (Part 3/6)

Acceptance Criteria:
- `AddEdge(from, to)` - Add dependency edge
- `GetReadyNodes()` - Return beads with in-degree 0
- `MarkCompleted(id)` - Mark bead done, reduce dependents' in-degree

### FR-002d: FR-002: DAG Data Structure (Part 4/6)

Acceptance Criteria:
- `TopologicalSort()` - Return execution order (Kahn's algorithm)
- `DetectCycle()` - Detect and return cycle path
- DAG supports 1000+ nodes efficiently

### FR-002e: FR-002: DAG Data Structure (Part 5/6)

Acceptance Criteria:
- In-degree calculation is accurate
- Thread-safe for concurrent access
- Memory usage is O(V + E)

### FR-002f: FR-002: DAG Data Structure (Part 6/6)

Acceptance Criteria:
- Cycle detection runs in O(V + E)
### FR-003a: FR-003: Dependency Resolution Integration (Part 1/7)

Acceptance Criteria:
- Build/refresh DAG on each poll cycle from `br list --json`
- Query DAG for ready beads instead of `br ready`
- Filter out already running beads

### FR-003b: FR-003: Dependency Resolution Integration (Part 2/7)

Acceptance Criteria:
- Sort ready beads by priority (high to low)
- Respect max workers limit
- Check if overseer is paused

### FR-003c: FR-003: Dependency Resolution Integration (Part 3/7)

Acceptance Criteria:
- Get active worker count and available slots
- Fetch all beads via `br list --json`
- Build/refresh DAG

### FR-003d: FR-003: Dependency Resolution Integration (Part 4/7)

Acceptance Criteria:
- Detect cycles (log and skip affected)
- Get ready beads from DAG (in-degree == 0)
- Filter out running, approval-gates, failed

### FR-003e: FR-003: Dependency Resolution Integration (Part 5/7)

Acceptance Criteria:
- Sort by priority
- Spawn workers for top-N beads
- DAG is rebuilt on each poll cycle

### FR-003f: FR-003: Dependency Resolution Integration (Part 6/7)

Acceptance Criteria:
- Only ready beads (all deps completed) are spawned
- Ready beads sorted by priority
- Cycle detection prevents infinite loops

### FR-003g: FR-003: Dependency Resolution Integration (Part 7/7)

Acceptance Criteria:
- Integration doesn't break existing API endpoints
### FR-004a: FR-004: Worker Completion Handling (Part 1/6)

Acceptance Criteria:
- On worker success: Mark bead completed in DAG
- On worker failure: Mark bead failed, don't unblock dependents
- Trigger next poll cycle immediately (bypass 5s interval)

### FR-004b: FR-004: Worker Completion Handling (Part 2/6)

Acceptance Criteria:
- Handle partial failures (some deps fail, others succeed)
- Support retry logic (configurable max retries)
- Worker process exits

### FR-004c: FR-004: Worker Completion Handling (Part 3/6)

Acceptance Criteria:
- MonitorWorker determines success/failure
- Update DAG node status
- If success: Reduce in-degree of all dependents

### FR-004d: FR-004: Worker Completion Handling (Part 4/6)

Acceptance Criteria:
- If new beads become ready: Trigger scheduling
- Update REST API stats
- Log completion with duration

### FR-004e: FR-004: Worker Completion Handling (Part 5/6)

Acceptance Criteria:
- DAG updated when worker completes
- Dependents unblocked only on success
- Immediate scheduling check triggered

### FR-004f: FR-004: Worker Completion Handling (Part 6/6)

Acceptance Criteria:
- Retry count tracked per bead
- Failed beads don't block forever (configurable timeout)
### FR-005a: FR-005: Error Handling and Recovery (Part 1/4)

Acceptance Criteria:
- Custom error types: DAGError, CycleError, CLIError
- Cycle detection prevents invalid graph state
- CLI failures include command output in error

### FR-005b: FR-005: Error Handling and Recovery (Part 2/4)

Acceptance Criteria:
- Retry with exponential backoff for transient failures
- Graceful degradation (continue with partial data)
- All errors implement error interface with context

### FR-005c: FR-005: Error Handling and Recovery (Part 3/4)

Acceptance Criteria:
- Cycle errors include full path
- CLI errors include stdout/stderr
- Transient errors trigger retry

### FR-005d: FR-005: Error Handling and Recovery (Part 4/4)

Acceptance Criteria:
- Fatal errors don't crash overseer
### FR-006a: FR-006: Testing Strategy (Part 1/6)

Acceptance Criteria:
- JSON parsing for various bead formats
- DAG construction with different topologies
- Cycle detection (positive and negative cases)

### FR-006b: FR-006: Testing Strategy (Part 2/6)

Acceptance Criteria:
- Topological sort validation
- Error handling paths
- End-to-end with mock `br` CLI

### FR-006c: FR-006: Testing Strategy (Part 3/6)

Acceptance Criteria:
- DAG integration with overseer
- Concurrent worker spawning
- Error recovery scenarios

### FR-006d: FR-006: Testing Strategy (Part 4/6)

Acceptance Criteria:
- DAG construction with 1000 nodes
- Cycle detection performance
- Ready bead query performance

### FR-006e: FR-006: Testing Strategy (Part 5/6)

Acceptance Criteria:
- >80% code coverage
- All edge cases tested
- Race detector passes

### FR-006f: FR-006: Testing Strategy (Part 6/6)

Acceptance Criteria:
- Benchmarks establish performance baselines
## 4. Non-Functional Requirements

### NFR-001: Performance

**Metrics:**
- DAG construction: <100ms for 1000 beads
- Cycle detection: <50ms for 1000 beads
- Ready bead query: <10ms
- JSON parsing: <5ms per bead
- Memory: <10MB for 1000 beads

### NFR-002: Scalability

**Limits:**
- Support up to 10,000 beads per project
- Support up to 100 projects
- Max 50 concurrent workers (configurable)
- Poll interval configurable (default 5s)

### NFR-003: Reliability

**Requirements:**
- Zero downtime during DAG updates
- Graceful handling of `br` CLI failures
- Automatic retry for transient errors
- No data loss on worker crashes

### NFR-004: Observability

**Requirements:**
- Structured logging for all operations
- Metrics: dag_build_duration, cycle_detected, ready_beads_count
- Trace IDs for workflow tracking
- REST API exposes DAG state

## 5. Technical Considerations

### Architecture

**New Files:**
- `internal/core/dag.go` - DAG implementation
- `internal/core/errors.go` - Custom error types
- `internal/beads/client_test.go` - Client tests
- `internal/core/dag_test.go` - DAG tests

**Modified Files:**
- `internal/beads/client.go` - Replace parsers with JSON
- `internal/core/overseer.go` - Integrate DAG
- `pkg/types/bead.go` - Add JSON tags, fix ParseBeadID

### Data Models

**BeadJSON (from CLI):**
```go
type BeadJSON struct {
    ID          string    `json:"id"`
    Title       string    `json:"title"`
    Description string    `json:"description"`
    Status      string    `json:"status"`
    Priority    int       `json:"priority"`
    IssueType   string    `json:"issue_type"`
    Labels      []string  `json:"labels"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
    Dependencies []struct {
        ID     string `json:"id"`
        Status string `json:"status"`
    } `json:"dependencies"`
}
```

**DAGNode:**
```go
type DAGNode struct {
    Bead       types.Bead
    InDegree   int
    OutEdges   []string // IDs of dependent beads
    Status     types.BeadStatus
}
```

### Dependencies

**Add:**
- `golang.org/x/sync/errgroup` - For concurrent validation (optional)

**Use Existing:**
- Standard library: `encoding/json`, `sync`, `context`
- Gin for API (already integrated)

## 6. Risks and Mitigation

| Risk | Impact | Mitigation |
|------|--------|------------|
| Beads CLI format changes | High | Use `--json` flag, version detection |
| Circular dependencies in production | Medium | Detection + logging, skip affected |
| Memory pressure with large graphs | Medium | Pagination, lazy loading |
| Race conditions in DAG | High | RWMutex, proper locking order |
| Performance degradation | Low | Benchmarks, profiling, optimization |

## 7. MoSCoW Prioritization

### Must Have
- FR-001: JSON parsing for all CLI commands
- FR-002: DAG data structure
- FR-003: Dependency resolution integration
- FR-004: Worker completion handling
- FR-005: Basic error handling

### Should Have
- FR-006: Comprehensive testing (>80% coverage)
- Cycle error reporting with full path
- Retry logic with exponential backoff
- Metrics and observability

### Could Have
- GraphQL API for DAG queries
- Visualization endpoint
- Historical DAG analysis
- Performance optimizations

### Won't Have
- Graph database backend
- Distributed DAG across multiple instances
- Real-time collaborative editing
- Machine learning for optimization

## 8. Success Metrics

**Functional:**
- 100% of ready beads correctly identified
- 0% false positives (blocked beads marked ready)
- <100ms DAG rebuild time for 1000 beads
- 100% cycle detection accuracy

**Performance:**
- <5% overhead compared to current implementation
- Memory usage <20MB for typical workloads
- API response time <50ms for stats endpoint

**Quality:**
- >80% test coverage
- Zero race conditions (verified with -race)
- Zero memory leaks (verified with pprof)

## 9. Research Insights

### Codebase Findings
- Beads CLI supports `--json` flag (use this, not text parsing)
- Bead IDs are strings (e.g., `hydra-zxc`), not sequential numbers
- Current `parseBeadDetails()` and `parseListOutput()` are stubbed
- Overseer already has polling loop and worker management

### Best Practices
- Use Kahn's algorithm for topological sort (O(V+E))
- Validate DAG on construction, not execution
- Use adjacency list representation
- Implement cycle detection with DFS

### Domain Insights
- Software workflows have sequential, parallel, fan-out, fan-in patterns
- Human approval gates pause workflow
- Retry logic essential for transient failures
- Observability critical for debugging dependencies

## 10. Implementation Phases

### Phase 1: Foundation (Week 1)
- Update BeadID type to string
- Implement JSON parsing for all CLI commands
- Add comprehensive tests for parsing

### Phase 2: DAG Implementation (Week 2)
- Create DAG data structure
- Implement graph operations (add, remove, sort)
- Add cycle detection
- Unit tests for DAG

### Phase 3: Integration (Week 3)
- Integrate DAG with overseer
- Modify polling loop to use DAG
- Handle worker completion
- Integration tests

### Phase 4: Polish (Week 4)
- Error handling improvements
- Performance optimization
- Metrics and logging
- Documentation
