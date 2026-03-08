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

## 5. Error Handling & Retry Strategy

### 5.1 Error Classification

**Transient Errors (Retryable):**
- Network timeouts (MCP, cass_memory, llm-tldr)
- Temporary resource unavailability
- Process spawn failures (rate limiting)
- Database locks (beads)

**Permanent Errors (Non-Retryable):**
- Invalid bead configuration
- Missing dependencies
- Permission denied
- Invalid agent type
- Bead not found

### 5.2 Retry Policy

```yaml
retry:
  max_attempts: 3                    # Maximum retry attempts
  backoff_strategy: exponential      # exponential, linear, fixed
  initial_delay: 1s                  # First retry delay
  max_delay: 30s                     # Maximum delay between retries
  jitter: true                       # Add randomness to prevent thundering herd
```

**Retry Logic:**
1. First attempt: Immediate execution
2. Second attempt: After 1s + jitter
3. Third attempt: After 2s + jitter
4. Fourth+ attempt: After 4s + jitter (capped at max_delay)

### 5.3 Circuit Breaker Pattern

For external services (MCP, cass_memory, llm-tldr):

```go
type CircuitBreaker struct {
    Name            string        // Service name
    FailureThreshold int          // Failures before opening
    SuccessThreshold int          // Successes before closing
    Timeout         time.Duration // Reset timeout
    State           State         // Closed, Open, HalfOpen
}

// States:
// Closed: Normal operation, requests pass through
// Open: Service unhealthy, requests fail fast
// HalfOpen: Testing if service recovered
```

**Configuration:**
```yaml
circuit_breakers:
  mcp:
    failure_threshold: 5
    success_threshold: 3
    timeout: 30s
  cass_memory:
    failure_threshold: 3
    success_threshold: 2
    timeout: 10s
  llm_tldr:
    failure_threshold: 3
    success_threshold: 2
    timeout: 10s
```

### 5.4 Error Propagation

**Worker Level:**
- Worker process returns exit code 0 for success, non-zero for failure
- Logs written to stdout/stderr (captured by overseer)
- Completion message sent via MCP with error details

**Overseer Level:**
- Detects worker failure via process exit code
- Updates bead status to `failed`
- Records failure in cass_memory (anti-pattern)
- Triggers retry if retry count < max_attempts
- Creates follow-up bead if retries exhausted

**API Level:**
- GraphQL errors include structured error codes
- Clients receive detailed error messages
- SSE streams error events

### 5.5 Dead Letter Queue (DLQ)

Failed beads that exceed retry limits:
- Moved to DLQ for manual inspection
- Preserved with full context (logs, error, retry history)
- Can be manually retried or archived
- DLQ persistence: SQLite database

---

## 6. Database Schema

Hydra uses SQLite for internal state management:

### 6.1 workers Table
```sql
CREATE TABLE workers (
    id TEXT PRIMARY KEY,              -- Worker UUID
    bead_id TEXT NOT NULL,            -- Reference to bead
    project TEXT NOT NULL,            -- Project name
    repo TEXT,                        -- Repo name (nullable for single repo)
    agent_type TEXT NOT NULL,         -- Agent type
    pid INTEGER NOT NULL,             -- Process ID
    status TEXT NOT NULL,             -- running, completed, failed
    start_time DATETIME NOT NULL,     -- Spawn timestamp
    end_time DATETIME,                -- Completion timestamp
    exit_code INTEGER,                -- Process exit code
    error_message TEXT,               -- Error details if failed
    retry_count INTEGER DEFAULT 0,    -- Number of retries
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 6.2 executions Table
```sql
CREATE TABLE executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    worker_id TEXT NOT NULL,          -- Reference to worker
    bead_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,         -- Attempt number
    status TEXT NOT NULL,             -- success, failed
    duration_ms INTEGER,              -- Execution time
    logs TEXT,                        -- Compressed logs
    error_details TEXT,               -- JSON error details
    cass_memory_entries INTEGER,      -- Number of patterns recorded
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    FOREIGN KEY (worker_id) REFERENCES workers(id)
);
```

### 6.3 metrics Table
```sql
CREATE TABLE metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    metric_name TEXT NOT NULL,        -- e.g., workers_active, success_rate
    metric_value REAL NOT NULL,
    project TEXT,                     -- Optional project filter
    labels TEXT                       -- JSON additional labels
);

-- Indexes
CREATE INDEX idx_workers_bead ON workers(bead_id);
CREATE INDEX idx_workers_status ON workers(status);
CREATE INDEX idx_executions_worker ON executions(worker_id);
CREATE INDEX idx_metrics_name_time ON metrics(metric_name, timestamp);
```

### 6.4 dlq Table (Dead Letter Queue)
```sql
CREATE TABLE dlq (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bead_id TEXT NOT NULL,
    project TEXT NOT NULL,
    repo TEXT,
    error_type TEXT NOT NULL,
    error_message TEXT NOT NULL,
    retry_count INTEGER NOT NULL,
    last_error_at DATETIME NOT NULL,
    context TEXT,                     -- JSON full context
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved BOOLEAN DEFAULT FALSE,
    resolved_at DATETIME
);
```

---

## 7. Security Model

### 7.1 Threat Model

**Assets:**
- Worker processes (ephemeral)
- File system (projects/repos)
- MCP Agent Mail (communication)
- cass_memory (patterns/history)
- Internal SQLite database

**Threats:**
1. Unauthorized worker execution
2. File system traversal (escape workspace)
3. MCP message injection
4. Resource exhaustion (DoS)
5. Secret leakage in logs

### 7.2 Security Controls

**Worker Isolation:**
- Workers run as separate processes
- Limited filesystem access (chroot recommended for production)
- No network access except via MCP
- Environment variables sanitized (no secrets)

**File System Security:**
```yaml
security:
  allowed_paths:
    - "~/source-code/"              # Only allow project directories
  denied_patterns:
    - "*.env*"                       # No environment files
    - "*.key"                        # No key files
    - ".ssh/*"                       # No SSH keys
  max_file_size: 100MB              # Prevent huge file processing
```

**MCP Security:**
- Message signing (optional)
- Rate limiting per agent
- Validate sender identity
- Audit log for sensitive operations

**Secrets Management:**
- Never log secrets (automatic redaction)
- Use environment variables for API keys
- Support for secret management systems (HashiCorp Vault, AWS Secrets Manager)

### 7.3 Authentication & Authorization

**Phase 1 (Current):**
- No authentication (single-user development)
- File system permissions enforced by OS

**Phase 2 (Future):**
- API key authentication for GraphQL API
- Role-based access control (RBAC)
- Audit logging

### 7.4 Audit Logging

All security-relevant events logged:
- Worker spawn/terminate
- File reservations (attempted, granted, denied)
- MCP authentication failures
- API access attempts

---

## 8. Technical Specifications

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

## 9. GraphQL Schema

### 9.1 Types

```graphql
# Core Types
type Bead {
  id: ID!
  project: String!
  repo: String
  number: Int!
  title: String!
  description: String
  type: String!
  status: BeadStatus!
  dependencies: [Bead!]!
  dependents: [Bead!]!
  createdAt: DateTime!
  updatedAt: DateTime!
  completedAt: DateTime
  metadata: JSON
}

enum BeadStatus {
  OPEN
  IN_PROGRESS
  COMPLETED
  FAILED
  BLOCKED
  APPROVAL_PENDING
}

type Worker {
  id: ID!
  bead: Bead!
  agentType: String!
  pid: Int!
  status: WorkerStatus!
  startTime: DateTime!
  endTime: DateTime
  duration: Int # milliseconds
  logs: [LogEntry!]!
}

enum WorkerStatus {
  RUNNING
  COMPLETED
  FAILED
  CANCELLED
}

type LogEntry {
  timestamp: DateTime!
  level: LogLevel!
  message: String!
}

enum LogLevel {
  DEBUG
  INFO
  WARN
  ERROR
}

type Project {
  name: String!
  path: String!
  type: RepoType!
  repos: [Repo!]!
  beads(status: BeadStatus): [Bead!]!
  stats: ProjectStats!
}

enum RepoType {
  SINGLE
  MULTI
}

type Repo {
  name: String!
  path: String!
  beads(status: BeadStatus): [Bead!]!
}

type ProjectStats {
  totalBeads: Int!
  completedBeads: Int!
  failedBeads: Int!
  activeWorkers: Int!
  successRate: Float!
  avgDuration: Int! # milliseconds
}

type SystemStats {
  activeWorkers: Int!
  maxWorkers: Int!
  totalProjects: Int!
  totalBeads: Int!
  queueDepth: Int! # Ready beads waiting
  uptime: Int! # seconds
  memoryUsage: Int! # bytes
  cpuUsage: Float! # percentage
}

type Error {
  code: String!
  message: String!
  details: JSON
}

# Input Types
input QueueBeadInput {
  beadId: ID!
  priority: Int
}

input SetMaxWorkersInput {
  count: Int!
}
```

### 9.2 Queries

```graphql
type Query {
  # Bead queries
  bead(id: ID!): Bead
  beads(
    project: String
    repo: String
    status: BeadStatus
    type: String
    limit: Int = 100
    offset: Int = 0
  ): [Bead!]!
  
  # Worker queries
  worker(id: ID!): Worker
  workers(
    project: String
    status: WorkerStatus
  ): [Worker!]!
  activeWorkers: [Worker!]!
  
  # Project queries
  project(name: String!): Project
  projects: [Project!]!
  
  # Stats
  stats: SystemStats!
  projectStats(project: String!): ProjectStats!
  
  # Workflow
  workflow(project: String!, repo: String): Workflow
  dependencyGraph(beadId: ID!): DependencyGraph
}

type Workflow {
  beads: [Bead!]!
  criticalPath: [Bead!]!
  estimatedCompletion: DateTime
}

type DependencyGraph {
  nodes: [Bead!]!
  edges: [DependencyEdge!]!
}

type DependencyEdge {
  from: ID!
  to: ID!
  type: DependencyType!
}

enum DependencyType {
  DEPENDS_ON
  BLOCKS
  PARALLEL
}
```

### 9.3 Mutations

```graphql
type Mutation {
  # Bead control
  queueBead(input: QueueBeadInput!): Bead!
  cancelBead(id: ID!): Bead!
  retryBead(id: ID!): Bead!
  
  # Worker control
  cancelWorker(id: ID!): Worker!
  killWorker(id: ID!, force: Boolean = false): Worker!
  
  # Overseer control
  pauseOverseer: Boolean!
  resumeOverseer: Boolean!
  setMaxWorkers(input: SetMaxWorkersInput!): SystemStats!
  
  # Configuration
  reloadConfig: Boolean!
  
  # DLQ management
  retryDLQEntry(id: ID!): Bead!
  archiveDLQEntry(id: ID!): Boolean!
  clearDLQ: Int! # Returns number cleared
}
```

### 9.4 Subscriptions

```graphql
type Subscription {
  # Bead updates
  beadUpdates(
    project: String
    repo: String
  ): BeadUpdate!
  
  # Worker updates
  workerUpdates: WorkerUpdate!
  
  # Logs
  logs(
    workerId: ID
    beadId: ID
    level: LogLevel = INFO
  ): LogEntry!
  
  # System stats
  statsUpdates(interval: Int = 5000): SystemStats!
  
  # Error events
  errors: Error!
}

type BeadUpdate {
  type: UpdateType!
  bead: Bead!
  previousStatus: BeadStatus
}

type WorkerUpdate {
  type: UpdateType!
  worker: Worker!
}

enum UpdateType {
  CREATED
  UPDATED
  DELETED
}
```

---

## 10. Deployment Guide

### 10.1 Prerequisites

**System Requirements:**
- Go 1.23 or higher
- SQLite 3
- 2GB RAM minimum (4GB recommended)
- 1GB disk space
- Linux/macOS/Windows

**External Services:**
- MCP Agent Mail (Port 8765)
- cass_memory (CLI available)
- llm-tldr (Port 8767)
- GPTCache (if enabled)

### 10.2 Installation

**From Source:**
```bash
# Clone repository
git clone https://github.com/buddhi/hydra.git
cd hydra

# Build
make build

# Install binary (optional)
sudo cp bin/hydra /usr/local/bin/
```

**Configuration:**
```bash
# Create config directory
mkdir -p ~/.config/hydra

# Copy example config
cp hydra.yaml.example ~/.config/hydra/hydra.yaml

# Edit configuration
nano ~/.config/hydra/hydra.yaml
```

### 10.3 Running Hydra

**Development Mode:**
```bash
# Start with debug logging
./bin/hydra -debug

# Start with custom config
./bin/hydra -config /path/to/hydra.yaml
```

**Production Mode:**
```bash
# Using systemd (Linux)
sudo systemctl enable hydra
sudo systemctl start hydra

# Using launchd (macOS)
# See scripts/hydra.plist

# Using PM2
pm2 start bin/hydra --name hydra
pm2 save
pm2 startup
```

### 10.4 Monitoring & Logging

**Log Configuration:**
```yaml
logging:
  level: info           # debug, info, warn, error
  format: json          # json, text
  output: stdout        # stdout, file, both
  file_path: /var/log/hydra/hydra.log
  max_size: 100MB       # Rotate when exceeded
  max_backups: 5        # Keep 5 rotated files
  max_age: 30           # Delete after 30 days
```

**Health Check Endpoint:**
```bash
# HTTP health check
curl http://localhost:8080/health

# Response:
# {
#   "status": "healthy",
#   "version": "1.0.0",
#   "uptime": 3600,
#   "active_workers": 3,
#   "queue_depth": 5
# }
```

### 10.5 Backup & Recovery

**What to Backup:**
- `~/.config/hydra/hydra.yaml` - Configuration
- `~/.config/hydra/hydra.db` - SQLite database (workers, executions)
- Project beads databases (`.beads/beads.db`)
- cass_memory database (`~/.cass/memory.db`)

**Backup Script:**
```bash
#!/bin/bash
# backup-hydra.sh

BACKUP_DIR="/backup/hydra/$(date +%Y%m%d)"
mkdir -p $BACKUP_DIR

cp ~/.config/hydra/hydra.yaml $BACKUP_DIR/
cp ~/.config/hydra/hydra.db $BACKUP_DIR/
cp ~/.cass/memory.db $BACKUP_DIR/

# Compress
tar -czf $BACKUP_DIR.tar.gz $BACKUP_DIR
rm -rf $BACKUP_DIR

echo "Backup created: $BACKUP_DIR.tar.gz"
```

### 10.6 Updating Hydra

**Zero-Downtime Update:**
1. Start new Hydra instance on different port
2. Wait for it to be ready (health check)
3. Switch load balancer to new instance
4. Gracefully stop old instance
5. Update port to default

**Standard Update:**
```bash
# Stop current instance
pkill hydra

# Backup
cp ~/.config/hydra/hydra.db ~/.config/hydra/hydra.db.bak

# Pull updates
git pull origin main

# Rebuild
make build

# Restart
./bin/hydra
```

---

## 11. Testing Strategy

### 11.1 Test Levels

**Unit Tests (80%+ Coverage):**
- Individual functions/methods
- Mocked dependencies
- Fast execution (<1s per test)
- Run in CI/CD pipeline

**Integration Tests:**
- Component interactions
- Database operations
- MCP client communication
- Test with real (but isolated) dependencies

**E2E Tests:**
- Full workflow testing
- Spawn real workers
- Test with real projects
- Validate end-to-end behavior

**Load Tests:**
- Simulate high worker count
- Measure throughput
- Identify bottlenecks
- Test recovery from failures

### 11.2 Test Organization

```
hydra/
├── internal/
│   ├── core/
│   │   ├── overseer.go
│   │   ├── overseer_test.go       # Unit tests
│   │   └── overseer_integration_test.go
│   └── ...
├── tests/
│   ├── integration/
│   │   ├── beads_client_test.go
│   │   ├── mcp_client_test.go
│   │   └── worker_test.go
│   ├── e2e/
│   │   ├── workflow_test.go
│   │   └── recovery_test.go
│   └── fixtures/
│       └── test_project/           # Test project for E2E
└── testutil/
    ├── mocks/                      # Mock implementations
    └── helpers.go                  # Test helpers
```

### 11.3 Unit Testing Guidelines

**Naming:**
- `TestFunctionName` - Basic functionality
- `TestFunctionName_Scenario` - Specific scenario
- `TestFunctionName_Error` - Error cases

**Example:**
```go
func TestOverseer_SpawnWorker_Success(t *testing.T) {
    // Arrange
    overseer := NewOverseer(testConfig())
    bead := testBead()
    
    // Act
    worker, err := overseer.spawnWorker(bead)
    
    // Assert
    require.NoError(t, err)
    assert.NotNil(t, worker)
    assert.Equal(t, bead.ID, worker.BeadID)
}

func TestOverseer_SpawnWorker_MaxWorkers(t *testing.T) {
    // Test max worker limit
}
```

**Mocking:**
```go
// Mock MCP client
type MockMCPClient struct {
    mock.Mock
}

func (m *MockMCPClient) SendMessage(msg Message) error {
    args := m.Called(msg)
    return args.Error(0)
}
```

### 11.4 Integration Testing

**Test Database:**
- Use in-memory SQLite for tests
- Reset state between tests
- Parallel test execution safe

**Test Services:**
- Start mock MCP server on random port
- Use test cass_memory database
- Mock llm-tldr responses

**Example:**
```go
func TestBeadsClient_Integration(t *testing.T) {
    // Setup test project
    testProject := createTestProject(t)
    defer cleanupTestProject(t, testProject)
    
    // Create test beads
    createTestBead(t, testProject, "test-bead-1")
    
    // Test client
    client := NewClient()
    beads, err := client.GetReadyBeads(testProject.Path, "")
    
    // Assert
    require.NoError(t, err)
    assert.Len(t, beads, 1)
    assert.Equal(t, "test-bead-1", beads[0].Title)
}
```

### 11.5 E2E Testing

**Workflow Tests:**
```go
func TestE2E_Workflow_SingleBead(t *testing.T) {
    // Setup
    overseer := startTestOverseer(t)
    defer overseer.Stop()
    
    project := createTestProject(t)
    bead := createTestBead(t, project, "implement", map[string]string{
        "test": "true",
    })
    
    // Execute
    err := overseer.triggerBead(bead.ID)
    require.NoError(t, err)
    
    // Wait for completion
    waitForBeadCompletion(t, bead.ID, 30*time.Second)
    
    // Assert
    completedBead := getBead(t, bead.ID)
    assert.Equal(t, types.StatusCompleted, completedBead.Status)
}
```

**Test Scenarios:**
1. Single bead execution
2. Dependency chain execution
3. Parallel bead execution
4. Approval gate workflow
5. Retry after failure
6. Worker cancellation
7. System recovery after crash

### 11.6 Performance Testing

**Load Test Scenarios:**
```go
func BenchmarkOverseer_SpawnWorkers(b *testing.B) {
    overseer := NewOverseer(testConfig())
    beads := generateTestBeads(b.N)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        overseer.spawnWorker(beads[i])
    }
}

func TestLoad_MaxWorkers(t *testing.T) {
    overseer := startTestOverseer(t)
    
    // Create 100 beads
    beads := createTestBeads(t, 100)
    
    // All should complete within timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    
    err := overseer.ProcessAll(ctx, beads)
    require.NoError(t, err)
}
```

### 11.7 CI/CD Integration

**GitHub Actions:**
```yaml
name: Tests

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.23'
      - run: go test -v ./... -coverprofile=coverage.out
      - run: go tool cover -func=coverage.out

  integration-tests:
    runs-on: ubuntu-latest
    services:
      mcp-agent-mail:
        image: mcp-agent-mail:latest
        ports:
          - 8765:8765
    steps:
      - uses: actions/checkout@v3
      - run: make test-integration

  e2e-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - run: make test-e2e
```

### 11.8 Test Data Management

**Test Projects:**
- `tests/fixtures/simple-project/` - Single repo
- `tests/fixtures/multi-repo/` - Multiple repos
- `tests/fixtures/large-project/` - Many beads for load tests

**Data Builders:**
```go
func NewBeadBuilder() *BeadBuilder {
    return &BeadBuilder{}
}

func (b *BeadBuilder) WithTitle(title string) *BeadBuilder {
    b.title = title
    return b
}

func (b *BeadBuilder) WithDependencies(deps ...string) *BeadBuilder {
    b.dependencies = deps
    return b
}

func (b *BeadBuilder) Build() types.Bead {
    return types.Bead{...}
}
```

---

## 12. Implementation Phases

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

## 13. Success Criteria

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

## 14. Open Questions

1. **Human Approval Flow**: Should approval gates show a prompt in CLI, or just wait for `br close`?

2. **Log Retention**: How long should worker logs be kept? Stream-only or persisted?

3. **Failure Strategy**: When a bead fails, should Hydra create a fix bead automatically, or just mark as failed?

4. **Tool Priority**: If multiple tools can handle a bead (opencode, Claude Code, aider), how to choose? Query cass_memory for historical success?

5. **Workspace Isolation**: One workspace = one repo? Or can workers work across repos?

---

## 15. Appendix

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
