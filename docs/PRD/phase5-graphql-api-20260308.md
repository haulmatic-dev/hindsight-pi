# Product Requirements Document: Phase 5 - GraphQL API

**Version:** 1.0  
**Date:** 2026-03-08  
**Status:** Ready for Implementation  

---

## 1. Executive Summary

Phase 5 adds a **GraphQL API** to Hydra, providing a flexible, type-safe query interface for clients. This replaces/supplements the REST API with:

- **Precise Data Fetching** - Clients request exactly what they need
- **Real-time Subscriptions** - WebSocket-based live updates
- **Strong Typing** - Schema-driven development
- **Single Endpoint** - All queries via `/graphql`

### Business Value

- **Reduced Payloads:** Fetch only required fields
- **Real-time Updates:** Live bead status, worker progress
- **Better Developer Experience:** Self-documenting schema
- **Mobile Optimization:** Efficient data transfer

---

## 2. User Stories

### US-001: Query Beads with Filtering
**As a** developer  
**I want** to query beads with complex filters  
**So that** I can build advanced UIs

**Acceptance Criteria:**
- Filter by status, priority, type, project
- Pagination support (cursor-based)
- Sort by multiple fields
- Return only requested fields

### US-002: Real-time Worker Updates
**As a** user  
**I want** to see worker status updates in real-time  
**So that** I can monitor task progress

**Acceptance Criteria:**
- Subscribe to worker status changes
- Receive updates when workers start/complete/fail
- WebSocket-based subscriptions
- Automatic reconnection

### US-003: Complex DAG Queries
**As a** developer  
**I want** to query the DAG with relationships  
**So that** I can visualize dependencies

**Acceptance Criteria:**
- Query beads with their dependencies
- Get ready beads with full context
- Query blocked beads with blocking reasons
- Get execution path for a bead

### US-004: Mutations for Control
**As a** user  
**I want** to control Hydra via GraphQL mutations  
**So that** I can pause/resume and manage workers

**Acceptance Criteria:**
- Pause/resume overseer
- Cancel specific workers
- Update configuration
- Force bead completion

---

## 3. Technical Requirements

### Technology Stack

- **GraphQL Server:** gqlgen (Go code generation)
- **Subscriptions:** WebSocket (gorilla/websocket)
- **Schema:** GraphQL SDL
- **Resolvers:** Go with existing services

### Schema Design

```graphql
# schema.graphqls

type Query {
  # Beads
  beads(
    filter: BeadFilter
    first: Int
    after: String
    sort: [BeadSort!]
  ): BeadConnection!
  
  bead(id: ID!): Bead
  readyBeads: [Bead!]!
  blockedBeads: [Bead!]!
  
  # Workers
  workers: [Worker!]!
  worker(id: ID!): Worker
  
  # DAG
  dagStats: DAGStats!
  executionPath(beadId: ID!): [Bead!]!
  
  # System
  health: Health!
  stats: SystemStats!
  config: Config!
}

type Mutation {
  # Overseer control
  pauseOverseer: Boolean!
  resumeOverseer: Boolean!
  setMaxWorkers(count: Int!): Int!
  
  # Worker control
  cancelWorker(id: ID!): Boolean!
  forceCompleteBead(id: ID!, reason: String): Bead!
  
  # Configuration
  updateConfig(input: ConfigInput!): Config!
}

type Subscription {
  # Real-time updates
  workerUpdates: Worker!
  beadUpdates(filter: BeadFilter): Bead!
  dagUpdates: DAGUpdate!
  logStream(workerId: ID): LogEntry!
}

# Types
type Bead {
  id: ID!
  title: String!
  description: String
  type: String!
  status: BeadStatus!
  priority: Int!
  project: String!
  repo: String
  dependencies: [Bead!]!
  dependents: [Bead!]!
  labels: [String!]!
  createdAt: Time!
  updatedAt: Time!
  completedAt: Time
  worker: Worker
}

type Worker {
  id: ID!
  bead: Bead!
  agentType: String!
  status: WorkerStatus!
  pid: Int!
  startTime: Time!
  endTime: Time
  logs: [LogEntry!]!
}

type LogEntry {
  timestamp: Time!
  level: LogLevel!
  message: String!
  workerId: ID
}

type DAGStats {
  totalNodes: Int!
  readyNodes: Int!
  blockedNodes: Int!
  completedNodes: Int!
  failedNodes: Int!
  activeWorkers: Int!
  maxWorkers: Int!
}

type SystemStats {
  uptime: Int!
  totalProjects: Int!
  overseerRunning: Boolean!
  paused: Boolean!
}

type Health {
  status: String!
  version: String!
  timestamp: Time!
}

# Enums
enum BeadStatus {
  OPEN
  IN_PROGRESS
  COMPLETED
  FAILED
  BLOCKED
}

enum WorkerStatus {
  PENDING
  RUNNING
  COMPLETED
  FAILED
  CANCELLED
}

enum LogLevel {
  DEBUG
  INFO
  WARN
  ERROR
}

# Inputs
input BeadFilter {
  status: [BeadStatus!]
  type: [String!]
  project: String
  priorityMin: Int
  priorityMax: Int
  labels: [String!]
  createdAfter: Time
  createdBefore: Time
}

input BeadSort {
  field: BeadSortField!
  direction: SortDirection!
}

enum BeadSortField {
  PRIORITY
  CREATED_AT
  UPDATED_AT
  TITLE
}

enum SortDirection {
  ASC
  DESC
}

input ConfigInput {
  maxWorkers: Int
  pollInterval: Int
  retryDelay: Int
}

# Pagination
type BeadConnection {
  edges: [BeadEdge!]!
  pageInfo: PageInfo!
  totalCount: Int!
}

type BeadEdge {
  node: Bead!
  cursor: String!
}

type PageInfo {
  hasNextPage: Boolean!
  hasPreviousPage: Boolean!
  startCursor: String
  endCursor: String
}

# Updates
type DAGUpdate {
  type: UpdateType!
  bead: Bead
  worker: Worker
  timestamp: Time!
}

enum UpdateType {
  BEAD_CREATED
  BEAD_UPDATED
  BEAD_COMPLETED
  WORKER_STARTED
  WORKER_COMPLETED
  WORKER_FAILED
}

# Scalars
scalar Time
```

---

## 4. Functional Requirements

### FR-001: GraphQL Server Setup

**Description:** Set up gqlgen and GraphQL endpoint.

**Requirements:**
- Install gqlgen: `go get github.com/99designs/gqlgen`
- Initialize gqlgen: `gqlgen init`
- Create schema file: `graph/schema.graphqls`
- Configure gqlgen: `gqlgen.yml`
- Add GraphQL handler to Gin router

**Files:**
```
graph/
├── schema.graphqls       # GraphQL schema
├── resolver.go           # Root resolver
├── generated/            # Generated code (by gqlgen)
│   └── generated.go
├── model/                # Generated models
│   └── models_gen.go
└── schema.resolvers.go   # Resolver implementations
```

### FR-002: Query Resolvers

**Description:** Implement query resolvers for data fetching.

**Resolvers to Implement:**

```go
// Query resolver
func (r *queryResolver) Beads(
    ctx context.Context,
    filter *model.BeadFilter,
    first *int,
    after *string,
    sort []*model.BeadSort,
) (*model.BeadConnection, error)

func (r *queryResolver) Bead(ctx context.Context, id string) (*model.Bead, error)
func (r *queryResolver) ReadyBeads(ctx context.Context) ([]*model.Bead, error)
func (r *queryResolver) BlockedBeads(ctx context.Context) ([]*model.Bead, error)
func (r *queryResolver) Workers(ctx context.Context) ([]*model.Worker, error)
func (r *queryResolver) Worker(ctx context.Context, id string) (*model.Worker, error)
func (r *queryResolver) DAGStats(ctx context.Context) (*model.DAGStats, error)
func (r *queryResolver) ExecutionPath(ctx context.Context, beadID string) ([]*model.Bead, error)
func (r *queryResolver) Health(ctx context.Context) (*model.Health, error)
func (r *queryResolver) Stats(ctx context.Context) (*model.SystemStats, error)
func (r *queryResolver) Config(ctx context.Context) (*model.Config, error)
```

### FR-003: Mutation Resolvers

**Description:** Implement mutation resolvers for control operations.

```go
// Mutation resolver
func (r *mutationResolver) PauseOverseer(ctx context.Context) (bool, error)
func (r *mutationResolver) ResumeOverseer(ctx context.Context) (bool, error)
func (r *mutationResolver) SetMaxWorkers(ctx context.Context, count int) (int, error)
func (r *mutationResolver) CancelWorker(ctx context.Context, id string) (bool, error)
func (r *mutationResolver) ForceCompleteBead(ctx context.Context, id string, reason *string) (*model.Bead, error)
func (r *mutationResolver) UpdateConfig(ctx context.Context, input model.ConfigInput) (*model.Config, error)
```

### FR-004: Subscription Resolvers

**Description:** Implement WebSocket-based subscriptions.

```go
// Subscription resolver
func (r *subscriptionResolver) WorkerUpdates(ctx context.Context) (<-chan *model.Worker, error)

func (r *subscriptionResolver) BeadUpdates(
    ctx context.Context,
    filter *model.BeadFilter,
) (<-chan *model.Bead, error)

func (r *subscriptionResolver) DAGUpdates(ctx context.Context) (<-chan *model.DAGUpdate, error)

func (r *subscriptionResolver) LogStream(
    ctx context.Context,
    workerID *string,
) (<-chan *model.LogEntry, error)
```

### FR-005: WebSocket Transport

**Description:** Configure WebSocket for subscriptions.

**Implementation:**
```go
// router.go
func setupGraphQL(server *api.Server) {
    // GraphQL handler with WebSocket support
    srv := handler.NewDefaultServer(
        generated.NewExecutableSchema(generated.Config{
            Resolvers: &graph.Resolver{
                Overseer: server.overseer,
            },
        }),
    )
    
    srv.AddTransport(transport.Websocket{
        Upgrader: websocket.Upgrader{
            CheckOrigin: func(r *http.Request) bool {
                return true // Configure for production
            },
            ReadBufferSize:  1024,
            WriteBufferSize: 1024,
        },
    })
    
    server.router.POST("/graphql", gin.WrapH(srv))
    server.router.GET("/graphql", gin.WrapH(srv))
    
    // GraphQL Playground
    server.router.GET("/playground", gin.WrapH(
        playground.Handler("Hydra GraphQL", "/graphql"),
    ))
}
```

### FR-006: Model Mapping

**Description:** Map internal types to GraphQL models.

```go
// graph/mapper.go
func ToGraphQLBead(bead types.Bead) *model.Bead {
    return &model.Bead{
        ID:          bead.ID.String(),
        Title:       bead.Title,
        Description: &bead.Description,
        Type:        bead.Type,
        Status:      model.BeadStatus(bead.Status),
        Priority:    bead.Priority,
        Project:     bead.Project,
        Repo:        &bead.Repo,
        Labels:      bead.Labels,
        CreatedAt:   bead.CreatedAt,
        UpdatedAt:   bead.UpdatedAt,
        CompletedAt: bead.CompletedAt,
    }
}

func ToGraphQLWorker(worker *types.Worker) *model.Worker {
    return &model.Worker{
        ID:        worker.ID,
        AgentType: worker.AgentType.String(),
        Status:    model.WorkerStatus(worker.Status),
        Pid:       worker.PID,
        StartTime: worker.StartTime,
        EndTime:   worker.EndTime,
    }
}
```

### FR-007: Event Broadcasting

**Description:** Broadcast events to subscriptions.

```go
// Internal event broadcaster
type EventBroadcaster struct {
    workerUpdates chan *model.Worker
    beadUpdates   chan *model.Bead
    dagUpdates    chan *model.DAGUpdate
    logUpdates    map[string]chan *model.LogEntry
}

func (eb *EventBroadcaster) BroadcastWorkerUpdate(worker *model.Worker) {
    select {
    case eb.workerUpdates <- worker:
    default:
        // Channel full, skip
    }
}

// In overseer, trigger broadcasts
func (o *Overseer) onWorkerStarted(worker *types.Worker) {
    gqlWorker := graph.ToGraphQLWorker(worker)
    o.eventBroadcaster.BroadcastWorkerUpdate(gqlWorker)
}
```

---

## 5. Implementation Plan

### Phase 5A: Setup (Day 1)
- Install gqlgen
- Create schema file
- Generate initial code
- Add GraphQL handler to router

### Phase 5B: Queries (Day 2-3)
- Implement bead queries
- Implement worker queries
- Implement DAG queries
- Add filtering and pagination

### Phase 5C: Mutations (Day 4)
- Implement control mutations
- Add validation
- Test mutations

### Phase 5D: Subscriptions (Day 5)
- Implement WebSocket transport
- Add subscription resolvers
- Wire up event broadcasting
- Test real-time updates

### Phase 5E: Testing (Day 6)
- Unit tests for resolvers
- Integration tests
- Subscription tests
- Load tests

---

## 6. API Endpoints

### GraphQL Endpoint

```
POST /graphql
GET  /graphql    # For subscriptions
```

### GraphQL Playground

```
GET /playground
```

### Example Queries

**Get ready beads:**
```graphql
query {
  readyBeads {
    id
    title
    priority
    type
  }
}
```

**Get bead with dependencies:**
```graphql
query {
  bead(id: "hydra-abc123") {
    id
    title
    status
    dependencies {
      id
      title
      status
    }
    worker {
      id
      status
      startTime
    }
  }
}
```

**Paginated beads with filter:**
```graphql
query {
  beads(
    filter: { status: [OPEN], type: ["implement"] }
    first: 10
    sort: [{ field: PRIORITY, direction: DESC }]
  ) {
    edges {
      node {
        id
        title
        priority
      }
      cursor
    }
    pageInfo {
      hasNextPage
      endCursor
    }
    totalCount
  }
}
```

**Subscribe to worker updates:**
```graphql
subscription {
  workerUpdates {
    id
    status
    bead {
      id
      title
    }
  }
}
```

**Pause overseer:**
```graphql
mutation {
  pauseOverseer
}
```

---

## 7. Testing

### Unit Tests
```go
func TestBeadResolver(t *testing.T) {
    resolver := &queryResolver{...}
    
    beads, err := resolver.Beads(context.Background(), nil, nil, nil, nil)
    assert.NoError(t, err)
    assert.NotNil(t, beads)
}
```

### Integration Tests
```bash
# Test query
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "{ health { status } }"}'

# Test subscription (via WebSocket)
wscat -c ws://localhost:8080/graphql
> {"type":"connection_init"}
> {"type":"start","id":"1","payload":{"query":"subscription { workerUpdates { id } }"}}
```

---

## 8. Success Metrics

- **Query Performance:** < 100ms for simple queries
- **Subscription Latency:** < 50ms for real-time updates
- **Concurrent Connections:** 100+ WebSocket connections
- **Schema Coverage:** 100% of REST API features

---

**Duration:** 1 week  
**Estimated Lines:** ~1,500 (generated + custom resolvers)
