# Product Requirements Document: Phase 3 - MCP Integration

**Version:** 1.0  
**Date:** 2026-03-08  
**Status:** Ready for Implementation  

---

## 1. Executive Summary

Phase 3 implements **MCP (Model Context Protocol) Agent Mail** integration for Hydra. MCP enables standardized communication between agents, file reservations, and task coordination. This allows Hydra workers to:

- **Register** with the MCP server on startup
- **Reserve files** to prevent conflicts between workers
- **Send/receive messages** for task coordination
- **Notify completion** via MCP messages
- **Stream logs** through MCP channels

### Business Value

- **Conflict Prevention:** File reservations prevent concurrent editing
- **Coordination:** Agents can communicate and coordinate work
- **Observability:** Real-time visibility into agent activities
- **Standardization:** Uses MCP protocol for interoperability

---

## 2. User Stories

### US-001: Agent Registration
**As a** worker process  
**I want** to register with the MCP server on startup  
**So that** the system knows I'm active and can route messages to me

**Acceptance Criteria:**
- Register with agent ID, bead ID, and capabilities
- Receive confirmation of successful registration
- Handle registration failures gracefully
- Auto-retry on transient failures

### US-002: File Reservation
**As a** worker  
**I want** to reserve files before editing them  
**So that** other workers don't modify the same files concurrently

**Acceptance Criteria:**
- Reserve specific files or glob patterns
- Set reservation duration (TTL)
- Check if files are already reserved
- Release reservations on completion
- Handle reservation conflicts

### US-003: Task Completion Notification
**As a** system  
**I want** workers to send completion messages via MCP  
**So that** the overseer and other agents are notified immediately

**Acceptance Criteria:**
- Send completion message with status and metadata
- Include execution duration and resource usage
- Support success, failure, and partial completion
- Message delivered reliably

### US-004: Log Streaming via MCP
**As a** developer  
**I want** to see worker logs in real-time via MCP  
**So that** I can monitor progress without polling files

**Acceptance Criteria:**
- Stream stdout/stderr through MCP
- Support structured log messages
- Include timestamps and log levels
- Buffer and retry on connection issues

### US-005: Message Routing
**As an** overseer  
**I want** to route messages to specific agents or broadcast  
**So that** I can coordinate work between agents

**Acceptance Criteria:**
- Send messages to specific agent by ID
- Broadcast to all agents
- Filter by bead type or project
- Message delivery confirmation

---

## 3. Functional Requirements

### FR-001: MCP Client

**Description:** Go client for MCP Agent Mail protocol.

**Requirements:**
- Create `internal/mcp/client.go`
- Implement WebSocket or HTTP client
- Support JSON-RPC 2.0 protocol
- Handle connection retries with exponential backoff
- Thread-safe operations

**Methods:**
```go
type Client struct {
    url        string
    agentID    string
    conn       *websocket.Conn
    mu         sync.RWMutex
}

func NewClient(url, agentID string) (*Client, error)
func (c *Client) Register(metadata RegistrationMetadata) error
func (c *Client) Deregister() error
func (c *Client) ReserveFiles(files []string, ttl time.Duration) (*Reservation, error)
func (c *Client) ReleaseFiles(files []string) error
func (c *Client) SendMessage(to string, msg Message) error
func (c *Client) Broadcast(msg Message) error
func (c *Client) StreamLog(level, message string) error
func (c *Client) Close() error
```

### FR-002: File Reservation System

**Description:** Manage file reservations to prevent conflicts.

**Requirements:**
- Reserve individual files or glob patterns
- Set TTL with automatic expiration
- Check reservation status
- Release on completion or timeout
- Handle conflicts (already reserved)

**Data Structures:**
```go
type Reservation struct {
    ID        string
    AgentID   string
    BeadID    string
    Files     []string
    ExpiresAt time.Time
}

type ReservationRequest struct {
    Files []string        `json:"files"`
    TTL   time.Duration   `json:"ttl"`
    BeadID string         `json:"bead_id"`
}
```

**Reservation Rules:**
- Files can only be reserved by one agent at a time
- Reservations expire after TTL (default 30m)
- Agents must release files when done
- Overseer can force-release stale reservations

### FR-003: Message Types

**Description:** Standardized message formats for MCP.

**Requirements:**
- Define message types for different purposes
- Include metadata (timestamp, sender, recipient)
- Support structured payloads

**Message Types:**
```go
const (
    MsgTypeRegistration   = "registration"
    MsgTypeDeregistration = "deregistration"
    MsgTypeTaskStart      = "task_start"
    MsgTypeTaskComplete   = "task_complete"
    MsgTypeLog            = "log"
    MsgTypeHeartbeat      = "heartbeat"
    MsgTypeError          = "error"
    MsgTypeCommand        = "command"
)

type Message struct {
    ID        string                 `json:"id"`
    Type      string                 `json:"type"`
    From      string                 `json:"from"`
    To        string                 `json:"to"`        // Empty for broadcast
    Timestamp time.Time              `json:"timestamp"`
    Payload   map[string]interface{} `json:"payload"`
}
```

### FR-004: Worker Integration

**Description:** Integrate MCP client with workers.

**Requirements:**
- Worker registers on startup
- Reserves files before execution
- Sends task start/completion messages
- Streams logs via MCP
- Deregisters on exit

**Worker Flow:**
1. **Startup:** Register with MCP, include bead ID and agent type
2. **Pre-execution:** Reserve target files
3. **During execution:** Stream logs periodically
4. **Completion:** Send task_complete message with status
5. **Cleanup:** Release file reservations, deregister

**Environment Variables:**
```bash
HYDRA_MCP_URL=http://127.0.0.1:8765
HYDRA_AGENT_ID=worker-{pid}-{timestamp}
```

### FR-005: Overseer Integration

**Description:** Overseer monitors and routes MCP messages.

**Requirements:**
- Overseer maintains MCP connection
- Listens for worker messages
- Routes commands to workers
- Tracks active reservations
- API endpoints for MCP status

**Overseer MCP Methods:**
```go
func (o *Overseer) StartMCPListener() error
func (o *Overseer) HandleMCPMessage(msg Message) error
func (o *Overseer) SendCommandToWorker(workerID string, cmd Command) error
func (o *Overseer) GetActiveReservations() []Reservation
func (o *Overseer) ForceReleaseReservation(reservationID string) error
```

### FR-006: MCP Configuration

**Description:** Configuration for MCP connection.

**Requirements:**
- Add to `hydra.yaml`:
```yaml
mcp:
  enabled: true
  url: "ws://127.0.0.1:8765"
  reconnect_interval: 5s
  max_reconnect_attempts: 10
  heartbeat_interval: 30s
  file_reservation_ttl: 30m
```

---

## 4. Non-Functional Requirements

### NFR-001: Reliability

- Auto-reconnect on connection failure
- Exponential backoff for retries
- Buffer messages during disconnection
- Graceful degradation if MCP unavailable

### NFR-002: Performance

- Message delivery < 100ms
- Support 50+ concurrent agents
- Handle 1000+ messages/minute
- Minimal overhead on worker execution

### NFR-003: Security

- Optional authentication tokens
- Validate sender identity
- Rate limiting on messages
- Audit logging

---

## 5. Technical Considerations

### Protocol

**MCP Agent Mail** uses:
- **Transport:** WebSocket or HTTP/2
- **Format:** JSON-RPC 2.0
- **Port:** 8765 (configurable)

### File Structure

```
internal/mcp/
├── client.go           # MCP client implementation
├── client_test.go
├── messages.go         # Message types and handlers
├── reservation.go      # File reservation logic
├── reservation_test.go
└── protocol.go         # MCP protocol constants

cmd/worker/
└── main.go             # Worker MCP integration

internal/core/
└── overseer.go         # Overseer MCP integration
```

### Dependencies

- Gorilla WebSocket (or standard library)
- JSON-RPC 2.0 handling
- Context for cancellation

### Error Handling

- Connection errors: Retry with backoff
- Reservation conflicts: Return error immediately
- Message delivery: Confirm receipt or retry
- Timeout: Cancel operation and cleanup

---

## 6. Implementation Phases

### Week 1: Core MCP Client
- Day 1-2: MCP client with WebSocket
- Day 3: Registration/deregistration
- Day 4: Message types and handlers
- Day 5: Tests and integration

### Week 2: File Reservations
- Day 1: Reservation system design
- Day 2: Reserve/release implementation
- Day 3: Conflict detection
- Day 4: Worker integration
- Day 5: Tests and API endpoints

---

## 7. Testing Strategy

### Unit Tests
- MCP client connection/reconnection
- Message serialization/deserialization
- Reservation lifecycle
- Error handling

### Integration Tests
- Worker registration flow
- End-to-end message delivery
- File reservation conflicts
- Overseer message routing

### Load Tests
- 50 concurrent agents
- 1000 messages/minute
- Connection drop/recovery

---

## 8. Success Metrics

- **Registration:** 100% success rate for workers
- **Message Delivery:** < 100ms latency
- **File Reservations:** Zero conflicts in production
- **Availability:** 99.9% uptime for MCP server

---

**Total Duration:** 2 weeks  
**Estimated Effort:** ~1,500 lines of code
