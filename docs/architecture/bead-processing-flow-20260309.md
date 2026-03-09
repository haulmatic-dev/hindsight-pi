# Bead Processing Flow

**Date**: 2026-03-09  
**Status**: Current  
**Related**: [System Architecture](./system-architecture-20260309.md), [Worker Lifecycle](./worker-lifecycle-20260309.md)

## Overview

This document describes the complete flow of processing a bead (task) from discovery to completion in the Hydra system.

## High-Level Flow

```mermaid
flowchart TB
    subgraph Discovery["1. Discovery Phase"]
        A[Beads Database] --> B[br ready]
        B --> C{Dependencies
        Resolved?}
        C -->|No| D[Blocked Queue]
        C -->|Yes| E[Ready Queue]
    end
    
    subgraph Scheduling["2. Scheduling Phase"]
        E --> F{Resources
        Available?}
        F -->|No| G[Wait for Resources]
        F -->|Yes| H[Spawn Worker]
    end
    
    subgraph Execution["3. Execution Phase"]
        H --> I[Initialize]
        I --> J[Register with MCP]
        J --> K[Execute Opencode]
        K --> L{Success?}
        L -->|Yes| M[Mark Complete]
        L -->|No| N{Retry?}
        N -->|Yes| O[Re-queue]
        N -->|No| P[Mark Failed]
    end
    
    subgraph Completion["4. Completion Phase"]
        M --> Q[Update DAG]
        P --> Q
        Q --> R[Unblock Dependents]
        R --> S{More Ready?}
        S -->|Yes| E
        S -->|No| T[End]
    end
    
    style Discovery fill:#e8f5e9
    style Scheduling fill:#fff3e0
    style Execution fill:#e3f2fd
    style Completion fill:#f3e5f5
```

## Phase 1: Discovery

### Bead Database Structure

```mermaid
erDiagram
    BEAD {
        string id
        string project
        string repo
        string title
        string description
        string status
        int priority
        array dependencies
        string type
    }
    
    DEPENDENCY {
        string bead_id
        string depends_on
        string status
    }
    
    BEAD ||--o{ DEPENDENCY : has
```

### Finding Ready Beads

**Command**: `br ready --json`

**Process**:
```mermaid
sequenceDiagram
    participant Overseer
    participant BeadsClient
    participant BeadsDB
    participant DAG
    
    loop Every poll_interval (5s)
        Overseer->>BeadsClient: GetReadyBeads()
        BeadsClient->>BeadsDB: br ready --json
        BeadsDB-->>BeadsClient: Ready beads list
        
        BeadsClient->>DAG: Check dependencies
        DAG-->>BeadsClient: InDegree = 0
        
        BeadsClient-->>Overseer: Filtered ready beads
    end
```

**Example Output**:
```json
[
  {
    "id": "opencode-simple-test-3a5",
    "project": "opencode-simple-test",
    "title": "Implement Basic Arithmetic",
    "status": "open",
    "priority": 5,
    "dependencies": [],
    "type": "feature"
  },
  {
    "id": "opencode-simple-test-1l3",
    "project": "opencode-simple-test",
    "title": "Add TypeScript Support",
    "status": "open",
    "priority": 5,
    "dependencies": ["opencode-simple-test-3v6"],
    "type": "developer"
  }
]
```

### Dependency Resolution

**DAG (Directed Acyclic Graph)**:
```mermaid
graph TD
    A[LINT GATE] --> B[SYNTAX Check]
    B --> C[RED: Tests]
    C --> D[GREEN: Implementation]
    D --> E[REFACTOR]
    
    F[FR-001 Part 1] --> G[FR-001 Part 2]
    G --> H[FR-001 Part 3]
    
    I[Developer] --> C
    
    style A fill:#ffebee
    style B fill:#fff3e0
    style C fill:#e3f2fd
    style D fill:#e8f5e9
    style E fill:#f3e5f5
```

**Ready Bead Criteria**:
1. Status = "open"
2. InDegree = 0 (no unresolved dependencies)
3. Not currently running
4. Not already completed
5. Not an approval-gate (manual approval required)

### Ready Queue Management

```go
func (o *Overseer) getReadyBeads() ([]types.Bead, error) {
    // 1. Query all ready beads
    allReady, err := o.beadsClient.GetReadyBeads(project.Path, "")
    
    // 2. Filter out running beads
    for _, bead := range allReady {
        if _, isRunning := o.activeWorkers[string(bead.ID)]; isRunning {
            continue  // Skip already running
        }
        
        // 3. Skip approval gates
        if bead.Type == "approval-gate" {
            continue
        }
        
        readyBeads = append(readyBeads, bead)
    }
    
    // 4. Sort by priority (high to low)
    sort.Slice(readyBeads, func(i, j int) bool {
        return readyBeads[i].Priority > readyBeads[j].Priority
    })
    
    return readyBeads, nil
}
```

## Phase 2: Scheduling

### Resource Check

```mermaid
flowchart LR
    A[Check Ready Beads] --> B[For Each Bead]
    B --> C{Active Workers
    < Max?}
    C -->|No| D[Skip]
    C -->|Yes| E{CPU < 70%?}
    E -->|No| F[Log: High CPU]
    E -->|Yes| G{Memory < 70%?}
    G -->|No| H[Log: High Memory]
    G -->|Yes| I[Spawn Worker]
```

**Resource Monitoring**:
```go
func (o *Overseer) tick() error {
    // ...
    for _, bead := range readyBeads {
        // Check resource limits
        if o.resourceMonitor != nil && !o.resourceMonitor.CanSpawnWorker() {
            log.Printf("[ResourceMonitor] Blocking worker spawn for bead %s", bead.ID)
            continue
        }
        
        if err := o.spawnWorker(bead); err != nil {
            // Handle error
        }
    }
}
```

### Worker Pool Management

```mermaid
flowchart TB
    subgraph WorkerPool["Worker Pool Management"]
        A[Max Workers: 10] --> B{Current Active?}
        B -->|7| C[Can Spawn: 3]
        B -->|10| D[Pool Full]
        D --> E[Queue Bead]
        E --> F[Retry Next Tick]
    end
```

### Priority Scheduling

Beads are processed by priority (highest first):

| Priority | Type | Examples |
|----------|------|----------|
| 10 | LINT GATE | Syntax checks, validation |
| 8 | SYNTAX | Performance checks |
| 5 | Developer | TypeScript support, tests |
| 3 | Feature | FR-001, FR-002, etc. |
| 1 | Approval | Manual gates |

## Phase 3: Execution

### Worker Initialization

**Environment Setup**:
```bash
# Environment variables passed to worker
export HYDRA_BEAD_ID="opencode-simple-test-3a5"
export HYDRA_PROJECT="opencode-simple-test"
export HYDRA_PROJECT_PATH="/tmp/hydra-test-..."
export HYDRA_REPO="main"
export HYDRA_AGENT_TYPE="orchestrator"
export HYDRA_BEAD_TITLE="Implement Basic Arithmetic"
export HYDRA_BEAD_DESCRIPTION="Add add, subtract functions..."
export HYDRA_RETRY_COUNT="0"
export HYDRA_MCP_ENABLED="true"
```

### Opencode Execution

**Command Construction**:
```go
func buildOpencodeCommand(bead types.Bead, projectPath string) *exec.Cmd {
    prompt := fmt.Sprintf("%s: %s", bead.Title, bead.Description)
    
    cmd := exec.Command("bash", "-c",
        fmt.Sprintf(`echo "%s" | opencode run --format json --dir "%s"`,
            prompt, projectPath))
    
    return cmd
}
```

**Activity Monitoring**:
```mermaid
sequenceDiagram
    participant Worker
    participant ActivityMonitor
    participant Opencode
    participant MCP
    
    Worker->>ActivityMonitor: Start monitoring
    Worker->>Opencode: Start process
    
    loop Every output line
        Opencode-->>Worker: stdout/stderr
        Worker->>ActivityMonitor: RecordActivity()
        Worker->>MCP: Stream log
    end
    
    alt No output for 5min
        ActivityMonitor->>Worker: Timeout!
        Worker->>Opencode: Kill process
    else Normal completion
        Opencode-->>Worker: Exit 0
        Worker->>Worker: Mark success
    end
```

### Execution Results

**Success**:
```json
{
  "success": true,
  "exit_code": 0,
  "output": "Generated calculator.js with add/subtract...",
  "files_created": ["calculator.js", "calculator.test.js"],
  "tests_passed": 5
}
```

**Failure**:
```json
{
  "success": false,
  "exit_code": 1,
  "error": "SyntaxError: Unexpected token...",
  "retry_eligible": true
}
```

## Phase 4: Completion

### Bead Closure

**Process**:
```mermaid
sequenceDiagram
    participant Worker
    participant BeadsClient
    participant BeadsDB
    participant DAG
    
    Worker->>BeadsClient: CloseBead(beadID, "completed")
    BeadsClient->>BeadsDB: br close bead-123 -r completed
    BeadsDB->>BeadsDB: Update status to "closed"
    BeadsDB-->>BeadsClient: OK
    
    BeadsClient->>BeadsClient: Verify closure
    BeadsClient->>BeadsDB: br show bead-123
    BeadsDB-->>BeadsClient: status=closed
    
    BeadsClient-->>Worker: Confirmed closed
    
    Worker->>DAG: Mark node complete
    DAG->>DAG: Update InDegree for dependents
```

### Dependency Unblocking

**Example**:
```mermaid
graph LR
    A[BEAD-1] --> B[BEAD-2]
    A --> C[BEAD-3]
    
    style A fill:#c8e6c9
    style B fill:#fff9c4
    style C fill:#fff9c4
```

When BEAD-1 closes:
1. BEAD-2 InDegree: 1 → 0 (now ready)
2. BEAD-3 InDegree: 1 → 0 (now ready)
3. Both added to ready queue next poll

### Retry Logic

**Flow**:
```mermaid
stateDiagram-v2
    [*] --> Execute: Start
    Execute --> CheckResult: Complete
    
    CheckResult --> Success: Exit 0
    CheckResult --> Failure: Exit != 0
    
    Failure --> RetryCheck{Retry < 2?}
    RetryCheck --> Requeue: Yes
    RetryCheck --> PermanentFail: No
    
    Requeue --> Delay: Wait 5s
    Delay --> Execute: Retry
    
    Success --> [*]
    PermanentFail --> [*]
```

## Performance Metrics

### Processing Rates

| Phase | Duration | Notes |
|-------|----------|-------|
| Discovery | ~100ms | `br ready` query |
| Scheduling | ~50ms | Resource check + spawn decision |
| Spawn | ~200ms | Process creation |
| Execution | 30s-3min | Opencode execution (variable) |
| Closure | ~100ms | `br close` + verification |
| **Total** | **31s-3.5min** | Per bead |

### Throughput

**With max_workers=10**:
- Theoretical: 10 beads × (60s/avg 90s) = ~6.7 beads/minute
- Actual: ~3 beads/minute (due to dependencies, retries)

**Scaling Factors**:
1. **Ready bead count**: Limited by dependencies
2. **System resources**: CPU/Memory constraints
3. **Opencode speed**: AI generation time
4. **Network**: MCP communication latency

## Error Handling

### Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| Activity Timeout | No output > 5min | Worker killed, retry |
| Resource Limit | CPU/Memory > 70% | Wait, retry next poll |
| Dependency Block | Unresolved deps | Wait for dependencies |
| Project Not Found | Config mismatch | Check hydra.yaml |
| MCP Error | Server down | Check `pm2 status` |

### Recovery Strategies

1. **Retry with backoff**: Wait 5s, try again
2. **Skip and continue**: Process other beads
3. **Log and alert**: Notify of persistent failures
4. **Resource cleanup**: Force GC, release memory

## Monitoring

### Key Metrics

```mermaid
flowchart TB
    subgraph Metrics["Key Metrics"]
        A[Beads Processed/min]
        B[Worker Success Rate]
        C[Average Execution Time]
        D[Resource Utilization]
        E[Queue Depth]
    end
    
    subgraph Alerts["Alert Conditions"]
        F{Success Rate < 50%?}
        G{Queue > 100?}
        H{Resources > 90%?}
    end
    
    A --> F
    E --> G
    D --> H
```

### Log Analysis

**Success Pattern**:
```
[Worker] Spawning worker for bead-123
[Worker] Completed: bead-123 (duration: 45.23s)
[CloseBead] Successfully closed bead-123
```

**Failure Pattern**:
```
[Worker] Spawning worker for bead-456
[Worker] Worker failed: bead-456 (exit: 1)
[CloseBead] Bead bead-456 not closed (status: open)
[Retry] Re-queueing bead-456 (attempt 1/2)
```

## Best Practices

1. **Dependency Management**: Keep dependency chains shallow
2. **Resource Planning**: Monitor CPU/Memory during peak loads
3. **Timeout Tuning**: 5min activity timeout works for most beads
4. **Retry Strategy**: 2 retries with 5s delay is usually sufficient
5. **Priority Assignment**: Use higher priority for blocking beads

## Related Documentation

- [System Architecture](./system-architecture-20260309.md) - High-level design
- [Worker Lifecycle](./worker-lifecycle-20260309.md) - Detailed worker states
- [Configuration Guide](../operations/configuration-guide-20260309.md) - Tuning performance
- [Troubleshooting](../operations/troubleshooting-20260309.md) - Debugging flow issues
