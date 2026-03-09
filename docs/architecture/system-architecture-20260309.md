# System Architecture

**Date**: 2026-03-09  
**Status**: Current  
**Related**: [Worker Lifecycle](./worker-lifecycle-20260309.md), [Bead Processing Flow](./bead-processing-flow-20260309.md)

## Overview

Hydra follows a modular, event-driven architecture with clear separation of concerns between the orchestration layer, intelligence services, and ephemeral workers.

## High-Level Architecture

```mermaid
flowchart TB
    subgraph Input["Input Layer"]
        Config["hydra.yaml<br/>Configuration"]
        BeadsDB[(".beads/<br/>Beads Database")]
    end
    
    subgraph Core["Hydra Core"]
        direction TB
        
        subgraph API["API Layer"]
            GraphQL["GraphQL API<br/>Queries & Subscriptions"]
            REST["REST Endpoints<br/>Health & Status"]
        end
        
        subgraph Orchestration["Orchestration Layer"]
            Overseer["Overseer Engine"]
            Scheduler["Bead Scheduler"]
            ResourceMonitor["Resource Monitor<br/>CPU/Memory 70%"]
            ActivityMonitor["Activity Monitor<br/>5min timeout"]
        end
        
        subgraph Intelligence["Intelligence Layer"]
            CassMemory["cass_memory<br/>Pattern Store"]
            TLDR["TLDR<br/>File Analysis"]
            GPTCache["GPTCache<br/>Response Cache"]
        end
        
        subgraph MCP["MCP Integration"]
            MCPClient["HTTP Client<br/>Streamable HTTP"]
            ReservationMgr["Reservation Manager<br/>File Locking"]
            MessageHandler["Message Handler"]
        end
    end
    
    subgraph Workers["Worker Layer"]
        WorkerPool["Worker Pool"]
        W1["Worker 1<br/>Opencode Agent"]
        W2["Worker 2<br/>Opencode Agent"]
        W3["Worker N..."]
    end
    
    subgraph External["External Systems"]
        MCPAgent["MCP Agent Mail<br/>Port 8765"]
        Opencode["Opencode CLI"]
    end
    
    Config --> Overseer
    BeadsDB --> Scheduler
    
    Overseer --> Scheduler
    Overseer --> ResourceMonitor
    Overseer --> ActivityMonitor
    Overseer --> Intelligence
    Overseer --> MCP
    
    GraphQL --> Overseer
    REST --> Overseer
    
    Scheduler --> WorkerPool
    WorkerPool --> W1
    WorkerPool --> W2
    WorkerPool --> W3
    
    W1 --> Opencode
    W2 --> Opencode
    W3 --> Opencode
    
    MCPClient --> MCPAgent
    MCPAgent -.->|File Reservations| Workers
    
    style Core fill:#e3f2fd
    style Workers fill:#fff8e1
    style Intelligence fill:#f3e5f5
    style MCP fill:#e8f5e9
```

## Component Details

### 1. API Layer

**GraphQL API** (`internal/api/`)
- Queries: beads, workers, status
- Subscriptions: real-time updates (TODO)
- Mutations: control operations

**REST Endpoints** (`internal/api/handlers.go`)
- `/health/liveness` - Health check
- `/health/readiness` - Ready state
- `/metrics` - Prometheus metrics (TODO)

### 2. Orchestration Layer

**Overseer** (`internal/core/overseer.go`)
- Central coordination hub
- Manages worker lifecycle
- Handles configuration
- Coordinates intelligence services

**Scheduler** (`internal/core/overseer.go`)
- Polls for ready beads (`br ready`)
- Maintains worker pool
- Enforces max_workers limit
- Respects dependency chains

**Resource Monitor** (`internal/core/resource_monitor.go`)
```mermaid
flowchart LR
    A[Check Resources] --> B{CPU < 70%?}
    B -->|Yes| C{Memory < 70%?}
    B -->|No| D[Block Spawn]
    C -->|Yes| E[Allow Spawn]
    C -->|No| D
    D --> F[Log: Resource limit exceeded]
    E --> G[Spawn Worker]
```

- Monitors CPU using `top` (cross-platform)
- Monitors Memory using `free` (Linux) or `vm_stat` (macOS)
- Blocks worker spawn when limits exceeded
- Platform-agnostic (standard Unix commands)

**Activity Monitor** (`internal/core/activity_monitor.go`)
```mermaid
sequenceDiagram
    participant Worker
    participant ActivityMonitor
    participant Opencode
    
    Worker->>ActivityMonitor: Start monitoring
    loop Every 10 seconds
        ActivityMonitor->>ActivityMonitor: Check last activity
        alt No activity > 5 min
            ActivityMonitor->>Opencode: Kill process
            ActivityMonitor->>Worker: Return timeout error
        else Activity detected
            ActivityMonitor->>ActivityMonitor: Reset timer
        end
    end
    Opencode->>ActivityMonitor: Output line
    ActivityMonitor->>ActivityMonitor: Record activity
    Opencode->>Worker: Complete
```

- Tracks stdout/stderr output
- Resets timeout on every line
- Kills process after 5 minutes of inactivity
- Prevents killing slow-but-active workers

### 3. Intelligence Layer

**cass_memory** (`internal/intelligence/cass_memory.go`)
- Pattern storage and retrieval
- Historical execution context
- Anti-pattern detection

**TLDR** (`internal/intelligence/tldr.go`)
- File impact analysis
- Dependency mapping
- Token optimization

**GPTCache** (`internal/intelligence/gptcache.go`)
- Response caching
- Cost optimization
- Rate limiting

### 4. MCP Integration

**HTTP Client** (`internal/mcp/http_client.go`)
- Streamable HTTP protocol (not WebSocket)
- Connection to port 8765
- JSON-RPC message format
- Auto-reconnection

**Reservation Manager** (`internal/mcp/reservation.go`)
- File locking mechanism
- Prevents concurrent edits
- TTL-based reservations

### 5. Worker Layer

**Worker Pool** (`cmd/worker/main.go`)
- Ephemeral workers
- One worker per bead
- Isolated environments
- Automatic cleanup

**Opencode Integration**
- Executes AI prompts
- Streams output to MCP
- Reports completion status
- Handles timeouts

## Data Flow

```mermaid
sequenceDiagram
    participant User
    participant Hydra as Hydra Overseer
    participant Scheduler
    participant ResourceMonitor
    participant Worker
    participant Opencode
    participant BeadsDB as Beads DB
    
    User->>Hydra: Start with config
    Hydra->>Scheduler: Initialize
    
    loop Poll Interval (5s)
        Scheduler->>BeadsDB: br ready
        BeadsDB-->>Scheduler: Ready beads
        
        Scheduler->>ResourceMonitor: Can spawn?
        ResourceMonitor-->>Scheduler: Yes/No
        
        alt Resources available
            Scheduler->>Worker: Spawn worker
            Worker->>Opencode: Execute
            
            loop Activity monitoring
                Opencode-->>Worker: Output
                Worker->>Worker: Reset timeout
            end
            
            Opencode-->>Worker: Complete
            Worker->>BeadsDB: br close
            Worker-->>Scheduler: Done
        else Resources limited
            Scheduler->>Scheduler: Skip, retry next poll
        end
    end
```

## Design Principles

1. **Resilience**: Workers are ephemeral and disposable
2. **Observability**: All components log extensively
3. **Resource Awareness**: Respects system limits
4. **Protocol Compliance**: Standard HTTP for MCP
5. **Cross-Platform**: Unix commands only, no external deps

## Related Documentation

- [Worker Lifecycle](./worker-lifecycle-20260309.md) - Detailed worker state machine
- [Bead Processing Flow](./bead-processing-flow-20260309.md) - End-to-end execution flow
- [Configuration Guide](../operations/configuration-guide-20260309.md) - Setup and tuning
