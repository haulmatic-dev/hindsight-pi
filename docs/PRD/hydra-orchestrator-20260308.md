# Hydra - Agent Swarm Orchestrator PRD

**Version:** 1.0  
**Date:** 2026-03-08  
**Status:** Phase 1 Implementation  
**Author:** buddhi

---

## 1. Executive Summary

Hydra is a self-improving agent swarm orchestrator designed for software development workflows. It spawns ephemeral agents (one per task/bead) that execute autonomously, learn from historical data, and terminate after completion.

### Key Differentiators
- **Ephemeral by Design**: One agent = one bead. Agent dies after completion.
- **Self-Improving**: Integrates cass_memory and llm-tldr for continuous learning
- **Multi-Repo Support**: Handles both single and multi-repo projects
- **Tool Agnostic**: Architecture allows swapping opencode with other tools
- **MCP-Native**: Uses MCP Agent Mail for all agent-to-agent communication

---

## 2. System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         HYDRA ORCHESTRATOR                                   │
│                    (Go - Single Machine Deployment)                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                     API SERVER (GraphQL + SSE)                        │   │
│  │  - Real-time task streaming via subscriptions                          │   │
│  │  - Query/mutation for external control                                 │   │
│  │  - WebSocket for live monitoring                                       │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                    │                                         │
│                                    ▼                                         │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                     OVERSEER (Core Scheduler)                         │   │
│  │  ┌─────────────┐  ┌──────────────┐  ┌──────────────┐                 │   │
│  │  │ Poll Beads  │  │ Dependency   │  │ Spawn        │                 │   │
│  │  │ (br ready)  │──▶│ Resolver     │──▶│ Workers      │                 │   │
│  │  │             │  │ (DAG)        │  │              │                 │   │
│  │  └─────────────┘  └──────────────┘  └──────────────┘                 │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                    │                                         │
│                                    ▼                                         │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                     INTELLIGENCE LAYER                                │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                │   │
│  │  │ cass_memory  │  │ llm-tldr     │  │ GPTCache     │                │   │
│  │  │ (cm)         │  │ (5-layer)    │  │ (LLM cache)  │                │   │
│  │  │              │  │              │  │              │                │   │
│  │  │ • Patterns   │  │ • Impact     │  │ • Responses  │                │   │
│  │  │ • History    │  │ • Context    │  │ • Context    │                │   │
│  │  │ • Anti-pat.  │  │ • Semantic   │  │ • Analysis   │                │   │
│  │  └──────────────┘  └──────────────┘  └──────────────┘                │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                    │                                         │
│                                    ▼                                         │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                     MCP AGENT MAIL (Port 8765)                        │   │
│  │  - Agent registration/deregistration                                  │   │
│  │  - File reservations (exclusive locks per repo)                       │   │
│  │  - Task completion/failure messages                                   │   │
│  │  - Cross-agent communication                                          │   │
│  │  - Persistent mailbox (survives worker death)                         │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                    │                                         │
│                                    ▼                                         │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                     EPHEMERAL WORKERS (One per Bead)                  │   │
│  │                                                                        │   │
│  │  Worker Lifecycle:                                                     │   │
│  │  1. Spawn with bead context                                            │   │
│  │  2. Query cass_memory: cm context "<bead>"                             │   │
│  │  3. Use llm-tldr: tldr_impact, tldr_semantic                           │   │
│  │  4. Determine agent type from bead stage                               │   │
│  │  5. Execute with opencode:                                             │   │
│  │     opencode --agent <type> --task <bead-id>                           │   │
│  │  6. Record patterns: cm add "<success>" --category <type>              │   │
│  │  7. Send completion via MCP                                            │   │
│  │  8. EXIT (process terminates)                                          │   │
│  │                                                                        │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Functional Requirements

### 3.1 Core Orchestration

**FR-001: Ephemeral Worker Model**
- Each worker processes exactly ONE bead
- Worker process terminates after completion (success or failure)
- No persistent state in workers
- Worker count limited by configuration (default: 6)

**FR-002: Dependency Resolution**
- Parse `depends_on` from bead metadata
- Build Directed Acyclic Graph (DAG)
- Block downstream tasks until dependencies complete
- Support parallel execution of independent tasks
- Detect and prevent circular dependencies

**FR-003: Approval Gates**
- Support `approval-gate` bead type
- Gate blocks all downstream dependencies
- Manual `br close <gate-id>` required to proceed
- Gate appears in monitoring API with "waiting" status

**FR-004: Multi-Repo Support**
- Auto-discover projects in `~/source-code/`
- Support single-repo: `~/source-code/project/.beads/`
- Support multi-repo: `~/source-code/project/{backend,frontend,mobile}/.beads/`
- Repo identifier: `{project-name}/{repo-name}-{number}` or `{project-name}-{number}`

**FR-005: Agent Type Selection**
Bead type → Agent mapping (from task-to-commit workflow):

| Bead Type | Primary Agent | Secondary Agents |
|-----------|--------------|------------------|
| requirements | orchestrator | prd |
| test | test-specialist | - |
| test-validate | test-validator | - |
| implement | orchestrator | - |
| refactor | orchestrator | fix-specialist |
| review | code-reviewer | adversarial-reviewer |
| deploy | deployment-specialist | - |
| fix | fix-specialist | - |
| approval-gate | human (manual) | - |

**FR-006: Dynamic Task Creation**
- Agents can inject new beads via MCP messages
- New beads appear in queue for processing
- Support parent-child relationships

### 3.2 Intelligence Integration

**FR-007: Pre-Execution Intelligence**
Before spawning worker:
1. Query cass_memory: `cm context "<bead-description>"`
2. Search patterns: `cm search "agent:<type> status:success"`
3. Analyze impact: `tldr_impact <files>`
4. Semantic search: `tldr_semantic "<query>"`

**FR-008: Post-Execution Learning**
After worker completion:
1. Record success: `cm add "<pattern>" --category <type>`
2. Record anti-pattern (if failed): `cm add "<failure>" --category <type> --anti`
3. Update metrics: duration, token usage, success rate

**FR-009: GPTCache Integration**
- Cache LLM responses for identical queries
- Reduce API costs and latency
- Cache key: hash of query + context

### 3.3 Communication Layer (MCP Agent Mail)

**FR-010: Agent Registration**
- Worker registers on spawn
- Includes: agent_id, task_id, project_key, timestamp
- Auto-deregister on exit

**FR-011: Task Messages**
- `task_assignment`: Initial task delegation
- `task_completion`: Success reporting with results
- `task_failure`: Error reporting with logs
- `status_update`: Progress updates (streaming)

**FR-012: File Reservations**
- Reserve files before execution: `reserve_file(repo, path, ttl)`
- Exclusive locks prevent parallel editing
- Auto-release on worker exit
- Support wildcard patterns: `src/**/*.ts`

**FR-013: Message Persistence**
- All messages stored in MCP mailbox
- Queryable history for debugging
- Survives worker/process restarts

### 3.4 Monitoring & API

**FR-014: GraphQL API**
Queries:
- `beads(status: String, repo: String)`: List beads
- `workflows(repo: String)`: Get workflow status
- `workers`: List active workers
- `stats`: System metrics

Mutations:
- `queueBead(id: ID!)`: Manually queue bead
- `pauseOverseer` / `resumeOverseer`
- `setMaxWorkers(count: Int!)`

Subscriptions:
- `beadUpdates`: Real-time bead state changes
- `workerUpdates`: Worker spawn/complete events
- `logs`: Streaming worker logs

**FR-015: Server-Sent Events (SSE)**
- Stream worker logs to API consumers
- Real-time status updates
- Low-latency monitoring

**FR-016: CLI Interface**
```bash
hydra start                 # Start overseer daemon
hydra status               # Show current status
hydra workers              # List active workers
hydra trigger <bead-id>    # Manually trigger bead
hydra logs <bead-id>       # Show logs
```

---

## 4. Non-Functional Requirements

**NFR-001: Performance**
- Overseer poll interval: 5 seconds (configurable)
- Max workers: 6 (configurable)
- Worker spawn time: < 1 second
- API response time: < 100ms

**NFR-002: Reliability**
- Graceful shutdown on SIGINT/SIGTERM
- Retry failed beads (configurable count)
- Zombie worker detection and cleanup
- Circuit breaker for MCP calls

**NFR-003: Scalability**
- Single machine deployment (Phase 1)
- Horizontal scaling via multiple machines (Future)
- Stateless overseer (can restart without data loss)

**NFR-004: Security**
- File reservations prevent conflicts
- No secrets in logs
- MCP authentication (if enabled)

**NFR-005: Observability**
- Structured logging (JSON)
- Metrics: worker count, success rate, latency
- Tracing via MCP message IDs

---

## 5. Technical Specifications

### 5.1 Technology Stack
- **Language**: Go 1.23+
- **Database**: SQLite (beads), MCP Agent Mail (messages)
- **Communication**: MCP Agent Mail (HTTP), GraphQL
- **Process Management**: Goroutines, os/exec
- **Configuration**: YAML

### 5.2 Project Structure
```
hydra/
├── cmd/
│   ├── hydra/              # Main daemon
│   └── worker/             # Worker binary
├── internal/
│   ├── core/
│   │   ├── overseer.go     # Main scheduler
│   │   ├── resolver.go     # DAG resolver
│   │   └── config.go       # Configuration
│   ├── beads/
│   │   └── client.go       # Beads CLI wrapper
│   ├── mcp/
│   │   ├── client.go       # MCP client
│   │   └── types.go        # Message types
│   ├── intelligence/
│   │   ├── cass.go         # cass_memory wrapper
│   │   └── tldr.go         # llm-tldr wrapper
│   ├── worker/
│   │   ├── executor.go     # Worker execution
│   │   └── agent.go        # Agent selection
│   └── api/
│       ├── server.go       # GraphQL server
│       └── resolvers.go    # GraphQL resolvers
├── pkg/
│   └── types/
│       └── bead.go         # Shared types
└── docs/PRD/
    └── hydra-orchestrator-20260308.md  # This document
```

### 5.3 Configuration
```yaml
# hydra.yaml
overseer:
  poll_interval: 5s
  max_workers: 6
  default_retries: 3
  worker_timeout: 30m

mcp:
  enabled: true
  port: 8765
  base_url: http://127.0.0.1:8765

api:
  enabled: true
  port: 8080

# Optional: Explicit project config
# (auto-discovery used if empty)
projects:
  - name: my-project
    path: /Users/buddhi/source-code/my-project
    type: single
    
  - name: microservices
    path: /Users/buddhi/source-code/microservices
    type: multi
    repos:
      - name: backend
        path: backend
      - name: frontend
        path: frontend
```

---

## 6. Implementation Phases

### Phase 1: Foundation (Current)
**Status:** In Progress
**Duration:** 1-2 weeks

**Deliverables:**
- [x] Project structure and Go modules
- [x] Overseer with basic polling loop
- [x] Project auto-discovery
- [x] Worker spawn with subprocess
- [ ] Beads client (br ready/show/close)
- [ ] Dependency resolver (DAG)
- [ ] Basic GraphQL API

### Phase 2: Intelligence Layer
**Status:** Not Started
**Duration:** 1 week

**Deliverables:**
- [ ] cass_memory integration (cm commands)
- [ ] llm-tldr integration (tldr_impact, tldr_semantic)
- [ ] GPTCache integration
- [ ] Pre-execution intelligence queries
- [ ] Post-execution learning loop

### Phase 3: MCP Integration
**Status:** Not Started
**Duration:** 1 week

**Deliverables:**
- [ ] MCP Agent Mail client
- [ ] Agent registration/deregistration
- [ ] Task completion messages
- [ ] File reservation system
- [ ] Message persistence

### Phase 4: Worker Logic
**Status:** Not Started
**Duration:** 1 week

**Deliverables:**
- [ ] Agent type determination
- [ ] Opencode execution
- [ ] Log streaming (stdout + MCP)
- [ ] Error handling and retries
- [ ] File reservation pre/post execution

### Phase 5: API & Monitoring
**Status:** Not Started
**Duration:** 1 week

**Deliverables:**
- [ ] GraphQL schema
- [ ] GraphQL resolvers
- [ ] Subscriptions (WebSocket)
- [ ] Server-Sent Events
- [ ] CLI commands
- [ ] Web UI (optional)

### Phase 6: Testing & Hardening
**Status:** Not Started
**Duration:** 1 week

**Deliverables:**
- [ ] Unit tests (>80% coverage)
- [ ] Integration tests
- [ ] E2E tests
- [ ] Load testing
- [ ] Documentation

---

## 7. Success Criteria

**Functional:**
- ✅ Spawns workers for ready beads automatically
- ✅ Respects dependencies and approval gates
- ✅ Workers terminate after completion
- ✅ MCP communication works
- ✅ File reservations prevent conflicts
- ✅ cass_memory and llm-tldr integration works

**Performance:**
- ✅ Overseer polls every 5s without lag
- ✅ Worker spawn time < 1s
- ✅ API response time < 100ms
- ✅ Handles 6 parallel workers smoothly

**Reliability:**
- ✅ Graceful shutdown
- ✅ Zombie detection
- ✅ Retry logic works
- ✅ No memory leaks over long runs

---

## 8. Open Questions

1. **Human Approval Flow**: Should approval gates show a prompt in CLI, or just wait for `br close`?

2. **Log Retention**: How long should worker logs be kept? Stream-only or persisted?

3. **Failure Strategy**: When a bead fails, should Hydra create a fix bead automatically, or just mark as failed?

4. **Tool Priority**: If multiple tools can handle a bead (opencode, Claude Code, aider), how to choose? Query cass_memory for historical success?

5. **Workspace Isolation**: One workspace = one repo? Or can workers work across repos?

---

## 9. Appendix

### A. Bead ID Format
- Single repo: `{project-name}-{number}` (e.g., `my-project-123`)
- Multi-repo: `{project-name}/{repo-name}-{number}` (e.g., `microservices/backend-456`)

### B. Worker Environment Variables
```bash
HYDRA_BEAD_ID={project}/{repo}-{number}
HYDRA_PROJECT={project-name}
HYDRA_REPO={repo-name}
HYDRA_AGENT_TYPE={agent-type}
HYDRA_MCP_URL=http://127.0.0.1:8765
```

### C. MCP Message Types
```go
type MessageType string

const (
    MsgTaskAssignment   MessageType = "task_assignment"
    MsgTaskCompletion   MessageType = "task_completion"
    MsgTaskFailure      MessageType = "task_failure"
    MsgStatusUpdate     MessageType = "status_update"
    MsgCreateBead       MessageType = "create_bead"
    MsgFileReserved     MessageType = "file_reserved"
    MsgFileReleased     MessageType = "file_released"
)
```

### D. References
- Opencode Task-to-Commit Workflow
- Opencode Orchestrator Architecture
- MCP Agent Mail Protocol
- cass_memory Documentation
- llm-tldr Documentation

---

**END OF PRD**

*This document is a living specification. Update as requirements evolve.*
