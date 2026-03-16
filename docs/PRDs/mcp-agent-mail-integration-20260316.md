# Product Requirements Document: MCP Agent Mail Integration Enhancement

**Version:** 1.0  
**Date:** 2026-03-16  
**Status:** Ready for Implementation  

---

## 1. Executive Summary

This PRD enhances Hydra's MCP (Model Context Protocol) Agent Mail integration with four key capabilities: **dynamic port discovery**, **context-processor agent**, **heartbeat monitoring with resource tracking**, and **multi-agent consensus** for complex architectural decisions.

### Business Value

- **Zero-Config Deployment**: Auto-discover MCP server without manual port configuration
- **Intelligent Execution**: Pre-process beads with context analysis for optimal routing
- **Operational Visibility**: Real-time worker health and resource monitoring
- **Better Decisions**: Multi-agent consensus for complex architectural changes
- **Conflict Prevention**: Queue-based file reservation with stuck-worker detection

### Current State vs Enhanced State

| Capability | Current | Enhanced |
|-----------|---------|----------|
| Port Configuration | Static (8765), often wrong | Auto-discovery with fallback |
| Pre-Execution | Basic CASS context injection | Full context-processor agent |
| Worker Monitoring | Process existence only | Heartbeat + resource metrics |
| File Conflicts | Warn and continue | Queue and wait |
| Complex Decisions | Single agent | Multi-agent consensus |

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

### US-002: Context-Processor Agent
**As a** system administrator  
**I want** a context-processor agent to analyze beads before execution  
**So that** each bead gets optimal routing and enriched context

**Acceptance Criteria:**
- Context-processor runs before every bead execution
- Queries cass-memory, llm-tldr, and bv for intelligence
- Decides: direct execution, specialist routing, or consensus
- Enriches prompt with relevant patterns and anti-patterns
- Stores analysis in cass-memory for future learning

### US-003: Heartbeat Monitoring
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

### US-005: Multi-Agent Consensus
**As a** architect  
**I want** complex beads to trigger multi-agent consensus  
**So that** architectural decisions are well-vetted

**Acceptance Criteria:**
- Context-processor triggers consensus for complex beads (>5 files or architectural changes)
- Facilitator agent spawns 2-4 expert reviewers
- Reviewers analyze independently then discuss
- Vote to approve, reject, or request changes
- Store consensus results in cass-memory

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

### FR-004a: Context-Processor Agent Implementation (Part 1/2)
Create dedicated context-processor agent that runs before each bead execution.

**Acceptance Criteria:**
- New agent type: context-processor
- Agent defined in .agents/prompts/context-processor.md
- Accepts bead via environment variables (HYDRA_BEAD_ID, etc.)

### FR-004b: Context-Processor Agent Implementation (Part 2/2)
Define the output format for the context-processor.

**Acceptance Criteria:**
- Outputs JSON with routing decision and enriched context

### FR-005a: Context-Processor Intelligence Queries (Part 1/2)
Implement intelligence gathering from cass-memory, llm-tldr, and bv.

**Acceptance Criteria:**
- Query cass-memory for patterns matching bead description
- Query llm-tldr for impact analysis of target files
- Query bv for dependency graph and related beads

### FR-005b: Context-Processor Intelligence Queries (Part 2/2)
Aggregate and handle query results robustly.

**Acceptance Criteria:**
- Aggregate results into structured context
- Handle query failures gracefully (log warning, continue)

### FR-006a: Context-Processor Decision Engine (Part 1/2)
Implement routing decision logic based on gathered intelligence.

**Acceptance Criteria:**
- Direct execution: 1-2 files, clear scope, no dependencies
- Specialist routing: 3-5 files, specific domain (testing, security, etc.)
- Consensus trigger: >5 files, architectural changes, 3+ dependencies

### FR-006b: Context-Processor Decision Engine (Part 2/2)
Provide transparency and confidence in decisions.

**Acceptance Criteria:**
- Include decision rationale in output
- Confidence score for each decision (0.0-1.0)

### FR-007a: Context-Processor Prompt Enrichment (Part 1/2)
Enrich execution prompts with gathered intelligence.

**Acceptance Criteria:**
- Inject relevant patterns from cass-memory
- Include anti-patterns to avoid
- Add file context from llm-tldr

### FR-007b: Context-Processor Prompt Enrichment (Part 2/2)
Complete enrichment with dependency context and logging.

**Acceptance Criteria:**
- Append dependency considerations from bv
- Log enriched prompt to file for debugging

### FR-008a: Context-Processor Result Storage (Part 1/2)
Store context-processor results in cass-memory for learning.

**Acceptance Criteria:**
- Store under category "context-analysis"
- Include: bead_id, strategy, patterns_found, decision_rationale
- Timestamp all entries

### FR-008b: Context-Processor Result Storage (Part 2/2)
Ensure traceability to original bead.

**Acceptance Criteria:**
- Reference original bead for traceability

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

### FR-014a: Multi-Agent Consensus Facilitation (Part 1/2)
Implement consensus facilitator agent for complex beads.

**Acceptance Criteria:**
- Spawn when context-processor decides consensus needed
- Select 2-4 expert reviewers based on bead characteristics
- Coordinate research phase (parallel queries to cass-memory, llm-tldr)

### FR-014b: Multi-Agent Consensus Facilitation (Part 2/2)
Manage discussion and voting.

**Acceptance Criteria:**
- Manage discussion rounds (3 rounds max)
- Collect and tally votes

### FR-015a: Multi-Agent Consensus Reviewer Selection (Part 1/2)
Implement dynamic reviewer selection based on bead analysis.

**Acceptance Criteria:**
- Testing beads → test-specialist + code-reviewer
- Security beads → security-auditor + code-reviewer
- Performance beads → performance-analyst + test-specialist

### FR-015b: Multi-Agent Consensus Reviewer Selection (Part 2/2)
Complete reviewer selection for architecture and health checks.

**Acceptance Criteria:**
- Architecture beads → orchestrator + code-reviewer + test-specialist
- Select from available healthy agents only

### FR-016a: Multi-Agent Consensus Voting (Part 1/2)
Implement voting mechanism for consensus decisions.

**Acceptance Criteria:**
- Reviewers vote: approve, reject, or needs_work
- Approve if 2/3 majority and no blocking concerns
- Reject if majority rejects

### FR-016b: Multi-Agent Consensus Voting (Part 2/2)
Handle edge cases and store vote details.

**Acceptance Criteria:**
- Needs_work if split or blocking concerns raised
- Store vote details: reviewer_id, vote, rationale, confidence

### FR-017a: Multi-Agent Consensus Result Storage (Part 1/2)
Store consensus results in cass-memory for learning.

**Acceptance Criteria:**
- Store under category "consensus-decisions"
- Include: bead_id, proposal, votes, outcome, dissenting_views
- Timestamp and duration

### FR-017b: Multi-Agent Consensus Result Storage (Part 2/2)
Reference all participants for traceability.

**Acceptance Criteria:**
- Reference all participating agents

---

## 4. Non-Functional Requirements

### NFR-001: Performance Requirements
**Acceptance Criteria:**
- Port discovery completes within 5 seconds
- Context-processor overhead < 10 seconds per bead
- Heartbeat latency < 100ms (worker to overseer)
- Consensus completion within 10 minutes
- No measurable impact on bead throughput

### NFR-002: Reliability Requirements
**Acceptance Criteria:**
- Port discovery success rate > 99%
- Heartbeat delivery rate > 95%
- Stuck worker detection within 2 minutes
- File reservation conflict rate < 1%
- Graceful degradation if cass-memory unavailable

### NFR-003: Scalability Requirements
**Acceptance Criteria:**
- Support 50+ concurrent workers
- Heartbeat aggregation scales to 100 workers
- Context-processor can query cass-memory concurrently
- Consensus sessions don't block other bead execution

### NFR-004: Observability Requirements
**Acceptance Criteria:**
- All port discovery attempts logged
- Context-processor decisions logged with rationale
- Heartbeat metrics exposed via GraphQL API
- Consensus votes auditable
- Debug logs for all reservation events

### NFR-005: Security Requirements
**Acceptance Criteria:**
- Port discovery only probes localhost (127.0.0.1)
- Heartbeats authenticated via MCP agent_id
- File reservations include bead_id for audit
- Consensus votes traceable to reviewer agents
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
| Context-Processor | Dedicated agent process | Clean separation, reusable |
| Heartbeat | MCP MsgTypeHeartbeat | Uses existing infrastructure |
| Consensus | Custom facilitator agent | Flexible, domain-specific |
| Storage | cass-memory | Existing institutional knowledge store |

### 5.3 Integration Points

**Existing Systems:**
- **MCP Client**: Extend for heartbeat sending
- **Overseer**: Add heartbeat aggregation and stuck worker detection
- **Cass-Memory**: Query patterns, store analysis results
- **LLM-TLDR**: Impact analysis and semantic search
- **BV**: Dependency graph and related beads

**New Components:**
- **Port Discovery**: internal/mcp/discovery.go
- **Context-Processor**: cmd/context-processor/main.go
- **Consensus Facilitator**: internal/consensus/facilitator.go

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

**Context-Processor Result:**
```go
type ContextResult struct {
    BeadID            string                 `json:"bead_id"`
    Strategy          string                 `json:"strategy"` // direct|specialist|consensus
    AgentType         string                 `json:"agent_type,omitempty"`
    ConsensusRequired bool                   `json:"consensus_required"`
    Patterns          []Pattern              `json:"patterns"`
    AntiPatterns      []Pattern              `json:"anti_patterns"`
    Context           map[string]interface{} `json:"context"`
    DecisionRationale string                 `json:"decision_rationale"`
    Confidence        float64                `json:"confidence"`
}
```

**Consensus Vote:**
```go
type ConsensusVote struct {
    ReviewerID string  `json:"reviewer_id"`
    Vote       string  `json:"vote"` // approve|reject|needs_work
    Rationale  string  `json:"rationale"`
    Confidence float64 `json:"confidence"`
}
```

---

## 6. Risks and Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| **Port discovery fails** | Medium | High | Fallback to config; default port 8766 |
| **Context-processor timeout** | Medium | Medium | Set 30s timeout; skip enrichment on failure |
| **Heartbeat flood** | Low | Medium | Rate limit heartbeats; batch in overseer |
| **Consensus deadlock** | Medium | High | 10-minute timeout; force decision by facilitator |
| **Stuck worker false positive** | Low | High | Require 3 missed heartbeats (90s); activity-based |
| **Cass-memory unavailable** | Low | Medium | Graceful degradation; use cached data |
| **Reviewer agent failure** | Medium | Medium | Retry with different agent; escalate to human |
| **Token cost explosion** | Medium | High | Monitor and alert on token usage; set budgets |

---

## 7. MoSCoW Prioritization

### Must Have (P0)
- FR-001: Port discovery via process parsing
- FR-002: Port discovery via health check scanning
- FR-004: Context-processor agent implementation
- FR-005: Context-processor intelligence queries
- FR-006: Context-processor decision engine
- FR-009: Worker heartbeat implementation
- FR-010: Overseer heartbeat aggregation
- FR-011: Stuck worker detection and recovery
- FR-012: File reservation queue and wait

### Should Have (P1)
- FR-003: Port discovery configuration override
- FR-007: Context-processor prompt enrichment
- FR-008: Context-processor result storage
- FR-013: File reservation steal from stuck worker
- FR-014: Multi-agent consensus facilitation
- FR-015: Multi-agent consensus reviewer selection

### Could Have (P2)
- FR-016: Multi-agent consensus voting
- FR-017: Multi-agent consensus result storage
- NFR enhancements (metrics dashboards, alerting)

### Won't Have (Future)
- Distributed consensus across multiple Hydra instances
- Machine learning for context-processor decisions
- Real-time collaborative editing (CRDTs)
- Cross-project bead dependencies

---

## 8. Success Metrics

### Operational Metrics
| Metric | Target | Measurement |
|--------|--------|-------------|
| Port discovery success rate | > 99% | Discovery attempts vs failures |
| Context-processor overhead | < 10s | Time from start to enriched prompt |
| Heartbeat delivery rate | > 95% | Heartbeats sent vs received |
| Stuck worker detection time | < 2 min | Time from stuck to detection |
| File conflict resolution | < 1% | Concurrent modification incidents |
| Consensus completion time | < 10 min | Time from trigger to decision |

### Quality Metrics
| Metric | Target | Measurement |
|--------|--------|-------------|
| Bead success rate improvement | +10% | Compare before/after implementation |
| Test pass rate | > 95% | Post-execution test results |
| Code review score | > B grade | Automated lint + style checks |
| Pattern reuse rate | > 30% | cass-memory hits per bead |

### Business Metrics
| Metric | Target | Measurement |
|--------|--------|-------------|
| Reduced manual intervention | -50% | Human intervention incidents |
| Token cost efficiency | -20% | Average tokens per bead |
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

**Existing Intelligence Integration:**
- cass-memory client: `internal/intelligence/cass_memory.go`
- TLDR client: `internal/intelligence/tldr.go`
- BV client: `internal/bv/client.go`
- Pattern: Skills-based queries (on-demand) vs force-fed context (deprecated)

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
3. **Consensus**: Hierarchical with facilitator + reviewers (not pure Raft/Paxos)
4. **File Locking**: Advisory locking with TTL + deadlock detection
5. **Context Processing**: Relevance filtering reduces noise by 60-80%

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

### Phase 1: Foundation (Port Discovery + Heartbeat)
**Duration:** 1-2 weeks
**Dependencies:** None
**Deliverables:**
- Port discovery with process parsing + scanning
- Worker heartbeat sending
- Overseer heartbeat aggregation
- Stuck worker detection

### Phase 2: Intelligence (Context-Processor)
**Duration:** 2-3 weeks
**Dependencies:** Phase 1
**Deliverables:**
- Context-processor agent
- cass-memory + llm-tldr + bv integration
- Decision engine
- Prompt enrichment
- Result storage

### Phase 3: Coordination (Consensus)
**Duration:** 2-3 weeks
**Dependencies:** Phase 2
**Deliverables:**
- Consensus facilitator agent
- Dynamic reviewer selection
- Voting mechanism
- Result storage

### Configuration Example

```yaml
mcp:
  enabled: true
  discovery:
    enabled: true
    scan_ports: [8765, 8766, 8767, 8768, 8769]
    default_port: 8766
    timeout: 5s

context_processor:
  enabled: true
  prompt_path: ".agents/prompts/context-processor.md"
  timeout: 30s
  store_results: true
  
heartbeat:
  interval: 30s
  timeout: 90s
  metrics: [cpu, memory, progress]

consensus:
  enabled: true
  min_reviewers: 2
  max_reviewers: 4
  timeout: 10m
  auto_trigger:
    file_threshold: 5
    dependency_threshold: 3
```

---

**End of PRD**
