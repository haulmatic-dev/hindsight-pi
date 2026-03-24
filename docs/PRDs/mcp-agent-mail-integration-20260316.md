# Product Requirements Document: MCP Agent Mail Integration Enhancement

**Version:** 1.0  
**Date:** 2026-03-16  
**Status:** Ready for Implementation  

---

## 1. Executive Summary

This PRD enhances Hydra's MCP (Model Context Protocol) Agent Mail integration with infrastructure capabilities: **dynamic port discovery**, **heartbeat monitoring with resource tracking**, and **conflict prevention** through queue-based file reservation with stuck-worker detection.

### Business Value

- **Zero-Config Deployment**: Auto-discover MCP server without manual port configuration
- **Operational Visibility**: Real-time worker health and resource monitoring
- **Conflict Prevention**: Queue-based file reservation with stuck-worker detection
- **Foundation for Intelligence**: Provides infrastructure layer for Hindsight-based context processing (separate PRD)

### Current State vs Enhanced State

| Capability | Current | Enhanced |
|-----------|---------|----------|
| Port Configuration | Static (8765), often wrong | Auto-discovery with fallback |
| Worker Monitoring | Process existence only | Heartbeat + resource metrics |
| File Conflicts | Warn and continue | Queue and wait |
| Stuck Worker Detection | None | Automatic detection and recovery |

---

## 2. User Stories

### US-001: Port Auto-Discovery
**As a** DevOps engineer  
**I want** Hydra to automatically discover the MCP Agent Mail server port  
**So that** I don't need to manually configure ports in hydra.yaml

**Acceptance Criteria:**
- Hydra discovers MCP port on startup without configuration
- Falls back to scanning if process parsing fails
- Uses default 8766 if no service found
- Logs discovered port for transparency

### US-002: Heartbeat Monitoring
**As a** system operator  
**I want** workers to send periodic health heartbeats  
**So that** I can detect stuck or failed workers

**Acceptance Criteria:**
- Workers send heartbeat every 30 seconds with metrics
- Metrics include: CPU %, memory MB, progress %, current phase
- Overseer marks worker "stuck" after 90 seconds without heartbeat
- Dashboard shows real-time worker health status
- Auto-terminate stuck workers and reopen beads

### US-004: File Conflict Resolution
**As a** developer  
**I want** workers to queue when files are reserved  
**So that** I avoid concurrent editing conflicts

**Acceptance Criteria:**
- Workers wait (queue) when target files are reserved
- Wait up to 5 minutes for files to be released
- If holder is stuck (no heartbeat), steal reservation
- Reopen stuck worker's bead for retry
- Log all reservation events for audit

---

## 3. Functional Requirements

### FR-001a: Port Discovery via Process Parsing (Part 1/2)
Implement cross-platform process discovery to find MCP Agent Mail port from running process command line arguments.

**Acceptance Criteria:**
- Parse macOS/Linux process list for "mcp-agent-mail" processes
- Extract --port argument from command line
- Handle multiple processes (use first match)

### FR-001b: Port Discovery via Process Parsing (Part 2/2)
Handle edge cases and errors in process parsing.

**Acceptance Criteria:**
- Return error if parsing fails (don't crash)

### FR-002a: Port Discovery via Health Check Scanning (Part 1/2)
Implement fallback port scanning when process parsing fails or returns no results.

**Acceptance Criteria:**
- Scan ports 8765-8769 with HTTP GET /health
- Return first port returning HTTP 200 with valid JSON
- Timeout each probe after 2 seconds

### FR-002b: Port Discovery via Health Check Scanning (Part 2/2)
Optimize port scanning to stop early on success.

**Acceptance Criteria:**
- Stop scanning after first successful match

### FR-003a: Port Discovery Configuration Override (Part 1/2)
Allow users to override auto-discovery via hydra.yaml configuration.

**Acceptance Criteria:**
- If mcp.port set in hydra.yaml, use configured value
- If mcp.discovery.enabled = false, skip auto-discovery
- Discovery only runs when mcp.enabled = true

### FR-003b: Port Discovery Configuration Override (Part 2/2)
Provide transparency about which discovery method was used.

**Acceptance Criteria:**
- Log whether using discovered or configured port

### FR-009a: Worker Heartbeat Implementation (Part 1/2)
Implement worker heartbeat sending to MCP server.

**Acceptance Criteria:**
- Send heartbeat every 30 seconds during execution
- Include: agent_id, bead_id, cpu_percent, memory_mb, progress_percent, current_phase
- Use mcp.MsgTypeHeartbeat message type

### FR-009b: Worker Heartbeat Implementation (Part 2/2)
Handle heartbeat failures gracefully.

**Acceptance Criteria:**
- Continue execution even if heartbeat fails (log warning)

### FR-010a: Overseer Heartbeat Aggregation (Part 1/2)
Implement heartbeat collection and health tracking in overseer.

**Acceptance Criteria:**
- Receive and store heartbeats from all workers
- Track last_heartbeat timestamp per worker
- Calculate health status: healthy, degraded (slow heartbeats), stuck (no heartbeat)

### FR-010b: Overseer Heartbeat Aggregation (Part 2/2)
Expose and maintain heartbeat data.

**Acceptance Criteria:**
- Expose metrics via GraphQL API
- Cleanup old heartbeat data after 24 hours

### FR-011a: Stuck Worker Detection and Recovery (Part 1/2)
Implement automatic detection and recovery of stuck workers.

**Acceptance Criteria:**
- Mark worker "stuck" after 90 seconds without heartbeat
- Kill stuck worker process
- Update bead status from "in_progress" to "open"

### FR-011b: Stuck Worker Detection and Recovery (Part 2/2)
Log incidents and clean up resources.

**Acceptance Criteria:**
- Log incident with worker_id, bead_id, timestamp
- Release all file reservations held by stuck worker

### FR-012a: File Reservation Queue and Wait (Part 1/2)
Implement queue-based file reservation with wait capability.

**Acceptance Criteria:**
- When files reserved, worker enters wait state
- Check reservation status every 10 seconds
- Maximum wait time: 5 minutes (configurable)

### FR-012b: File Reservation Queue and Wait (Part 2/2)
Handle wait completion or timeout.

**Acceptance Criteria:**
- If timeout, return error and fail bead
- If files released, proceed with reservation

### FR-013a: File Reservation Steal from Stuck Worker (Part 1/2)
Implement reservation stealing when holder is stuck.

**Acceptance Criteria:**
- Detect if file holder has missed 3+ heartbeats
- Kill stuck holder process
- Transfer reservation to waiting worker

### FR-013b: File Reservation Steal from Stuck Worker (Part 2/2)
Handle bead state and audit logging.

**Acceptance Criteria:**
- Reopen stuck holder's bead (status: open)
- Audit log: stolen_from_worker, stolen_by_worker, file, timestamp

---

## 4. Non-Functional Requirements

### NFR-001: Performance Requirements
**Acceptance Criteria:**
- Port discovery completes within 5 seconds
- Heartbeat latency < 100ms (worker to overseer)
- No measurable impact on bead throughput

### NFR-002: Reliability Requirements
**Acceptance Criteria:**
- Port discovery success rate > 99%
- Heartbeat delivery rate > 95%
- Stuck worker detection within 2 minutes
- File reservation conflict rate < 1%

### NFR-003: Scalability Requirements
**Acceptance Criteria:**
- Support 50+ concurrent workers
- Heartbeat aggregation scales to 100 workers

### NFR-004: Observability Requirements
**Acceptance Criteria:**
- All port discovery attempts logged
- Heartbeat metrics exposed via GraphQL API
- Debug logs for all reservation events

### NFR-005: Security Requirements
**Acceptance Criteria:**
- Port discovery only probes localhost (127.0.0.1)
- Heartbeats authenticated via MCP agent_id
- File reservations include bead_id for audit
- No sensitive data in logs (mask API keys)

---

## 5. Technical Considerations

### 5.1 Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     HYDRA ORCHESTRATOR                       │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │ Port         │  │ Context      │  │ Heartbeat    │       │
│  │ Discovery    │  │ Processor    │  │ Monitor      │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
│                                                               │
│  ┌─────────────────────────────────────────────────────────┐│
│  │              Multi-Agent Consensus                       ││
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐                ││
│  │  │Reviewer 1│ │Reviewer 2│ │Reviewer N│                ││
│  │  └──────────┘ └──────────┘ └──────────┘                ││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   EPHEMERAL WORKERS                          │
│  Worker 1    Worker 2    Worker 3    ...    Worker N         │
│  (heartbeat) (heartbeat) (heartbeat)        (heartbeat)     │
└─────────────────────────────────────────────────────────────┘
```

### 5.2 Technology Choices

| Component | Technology | Rationale |
|-----------|-----------|-----------|
| Port Discovery | Process parsing + HTTP probes | Cross-platform, no external dependencies |
| Heartbeat | MCP MsgTypeHeartbeat | Uses existing infrastructure |
| File Reservation | Queue + TTL-based locking | Prevents conflicts, handles stuck workers |
| Intelligence Layer | Hindsight (separate PRD) | Biomimetic memory system |

### 5.3 Integration Points

**Existing Systems:**
- **MCP Client**: Extend for heartbeat sending
- **Overseer**: Add heartbeat aggregation and stuck worker detection
- **Hindsight**: Intelligence layer for context processing (separate PRD)

**New Components:**
- **Port Discovery**: internal/mcp/discovery.go
- **File Reservation Manager**: Enhanced reservation system with queue support

### 5.4 Data Models

**Heartbeat Message:**
```go
type HeartbeatMessage struct {
    AgentID         string    `json:"agent_id"`
    BeadID          string    `json:"bead_id"`
    Timestamp       time.Time `json:"timestamp"`
    CPUPercent      float64   `json:"cpu_percent"`
    MemoryMB        int64     `json:"memory_mb"`
    ProgressPercent int       `json:"progress_percent"`
    CurrentPhase    string    `json:"current_phase"`
}
```

---

## 6. Risks and Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| **Port discovery fails** | Medium | High | Fallback to config; default port 8766 |
| **Heartbeat flood** | Low | Medium | Rate limit heartbeats; batch in overseer |
| **Stuck worker false positive** | Low | High | Require 3 missed heartbeats (90s); activity-based |
| **File reservation deadlock** | Low | High | TTL-based expiration; stuck worker detection |
| **Network partition** | Low | High | Heartbeat-based detection; automatic recovery |

---

## 7. MoSCoW Prioritization

### Must Have (P0)
- FR-001: Port discovery via process parsing
- FR-002: Port discovery via health check scanning
- FR-009: Worker heartbeat implementation
- FR-010: Overseer heartbeat aggregation
- FR-011: Stuck worker detection and recovery
- FR-012: File reservation queue and wait

### Should Have (P1)
- FR-003: Port discovery configuration override
- FR-013: File reservation steal from stuck worker
- NFR enhancements (metrics dashboards, alerting)

### Could Have (P2)
- Advanced file reservation policies (priority-based, deadline-aware)
- Extended heartbeat metrics (disk I/O, network latency)

### Won't Have (Future)
- Distributed consensus across multiple Hydra instances
- Real-time collaborative editing (CRDTs)
- Cross-project bead dependencies

---

## 8. Success Metrics

### Operational Metrics
| Metric | Target | Measurement |
|--------|--------|-------------|
| Port discovery success rate | > 99% | Discovery attempts vs failures |
| Heartbeat delivery rate | > 95% | Heartbeats sent vs received |
| Stuck worker detection time | < 2 min | Time from stuck to detection |
| File conflict resolution | < 1% | Concurrent modification incidents |

### Quality Metrics
| Metric | Target | Measurement |
|--------|--------|-------------|
| Bead success rate improvement | +10% | Compare before/after implementation |
| Test pass rate | > 95% | Post-execution test results |
| Code review score | > B grade | Automated lint + style checks |

### Business Metrics
| Metric | Target | Measurement |
|--------|--------|-------------|
| Reduced manual intervention | -50% | Human intervention incidents |
| Developer satisfaction | > 4.0/5 | Post-implementation survey |

---

## 9. Research Insights

### 9.1 Codebase Analysis Findings

**Current MCP Implementation:**
- WebSocket client exists but HTTP client is actively used (Streamable HTTP protocol)
- File reservation system with TTL (30 min default) already implemented
- Message types defined but command messages unused
- Reservation manager thread-safe with sync.RWMutex

**Key Integration Points:**
- `internal/mcp/client.go` - WebSocket client (659 lines)
- `internal/mcp/http_client.go` - HTTP client (238 lines)
- `internal/mcp/messages.go` - Message types (452 lines)
- `internal/mcp/reservation.go` - Reservation system (564 lines)
- `cmd/worker/main.go` - Worker MCP integration (939 lines)
- `internal/core/overseer.go` - Overseer MCP integration (3667 lines)

**Related PRDs:**
- Hindsight Integration: Context-processor and intelligence layer (separate PRD)
- Uses Hindsight for pattern storage and retrieval (replaces cass_memory)

### 9.2 Historical Lessons Learned

**From Git History Analysis:**
1. **Protocol Mismatch**: Initially built WebSocket, discovered MCP uses HTTP - always verify protocols first
2. **DAG → br ready**: Complex DAG system had stale data; simpler external tool more reliable
3. **Force-Fed → Skills**: 75-90% token savings by letting agents query vs pre-loading context
4. **Activity-Based Timeouts**: Static timeouts kill active workers; activity-based prevents false positives
5. **Process Group Management**: Pi spawns children; must kill entire process group (Setpgid)

**Patterns to Follow:**
- Graceful degradation (continue if intelligence unavailable)
- Atomic operations (prevent race conditions)
- Exponential backoff (prevent worker explosions)
- JSON-based state persistence

### 9.3 Best Practices Applied

**From Industry Research:**
1. **Service Discovery**: Process parsing + health probes (Kubernetes pattern)
2. **Health Checking**: Four Golden Signals (Google SRE) - latency, traffic, errors, saturation
3. **File Locking**: Advisory locking with TTL + deadlock detection
4. **Resource Monitoring**: cgroups-based metrics collection
5. **Queue Management**: Priority-based scheduling with timeout handling

**Go Libraries Recommended:**
- `github.com/hashicorp/memberlist` - Gossip protocol for discovery
- `github.com/gofrs/flock` - File locking
- `github.com/cenkalti/backoff/v4` - Retry strategies
- `go.opentelemetry.io/otel` - Distributed tracing

### 9.4 Domain-Specific Insights

**Multi-Agent AI Challenges:**
- Context synchronization critical for consistency
- O(n²) message overhead with agent count
- Token costs scale with both agents and context
- Hierarchical review beats flat voting

**Code Generation Best Practices:**
- Context is king: quality > model capability
- Prevent > Detect > Resolve for conflicts
- Test everything: automated gates essential
- Metrics-driven: measure before optimizing

---

## 10. Implementation Notes

### Phase 1: Foundation (Port Discovery + Heartbeat + File Reservation)
**Duration:** 2-3 weeks
**Dependencies:** None
**Deliverables:**
- Port discovery with process parsing + scanning
- Worker heartbeat sending
- Overseer heartbeat aggregation
- Stuck worker detection
- File reservation queue and wait

### Phase 2: Enhancement (Configuration + Reliability)
**Duration:** 1-2 weeks
**Dependencies:** Phase 1
**Deliverables:**
- Configuration override for port discovery
- File reservation steal from stuck workers
- Enhanced observability and metrics

### Configuration Example

```yaml
mcp:
  enabled: true
  discovery:
    enabled: true
    scan_ports: [8765, 8766, 8767, 8768, 8769]
    default_port: 8766
    timeout: 5s
  
heartbeat:
  interval: 30s
  timeout: 90s
  metrics: [cpu, memory, progress]

file_reservation:
  enabled: true
  max_wait_time: 5m
  steal_on_stuck: true
```

---

**End of PRD**
