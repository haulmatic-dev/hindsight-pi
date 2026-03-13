# Product Requirements Document: Multi-Agent Consensus System

**Date:** March 10, 2026  
**Version:** 1.0  
**Status:** Draft  
**Feature:** Consensus-Driven Collaborative Decision Making

---

## 1. Executive Summary

### Overview
Hydra currently orchestrates individual agents executing beads in isolation. This feature introduces **collaborative consensus sessions** where multiple specialized agents engage in facilitated discussions to reach well-researched, multi-perspective decisions on complex topics. The consensus outcome is captured as actionable recommendations with risk analysis, serving as institutional knowledge for future work.

### Business Value
- **Higher Quality Decisions**: Multiple expert perspectives catch blind spots
- **Reduced Technical Debt**: Early identification of architectural risks
- **Knowledge Preservation**: Decisions captured as searchable patterns in CASS
- **Traceability**: Full conversation history preserved for audit and learning
- **Scalable Expertise**: Consensus scales expertise without requiring senior engineer time

### Success Metrics
- 90%+ of consensus sessions reach conclusion within 4 rounds
- Average confidence score >0.8 for consensus recommendations
- 50%+ reduction in post-implementation architectural issues
- Zero context leakage to non-participating agents

---

## 2. User Stories

### Story 1: Automatic Consensus Triggering
**As a** product manager writing a PRD,  
**I want** to mark certain decisions as needing multi-agent consensus,  
**So that** complex architectural choices get proper expert review automatically.

**Acceptance Criteria:**
- [ ] PRD can specify `consensus: required` for any bead
- [ ] Consensus configuration includes topic, participants, and scope
- [ ] System automatically triggers consensus session when bead becomes ready
- [ ] Non-consensus beads process normally without overhead

### Story 2: Facilitated Discussion
**As a** consensus participant,  
**I want** a dedicated facilitator to guide the discussion,  
**So that** conversations stay focused and reach productive conclusions.

**Acceptance Criteria:**
- [ ] Facilitator agent initializes discussion with context
- [ ] Facilitator assigns speaking turns to expert agents
- [ ] Facilitator detects convergence and calls for consensus
- [ ] Facilitator synthesizes final recommendation

### Story 3: Comprehensive Research
**As an** expert agent in consensus,  
**I want** to research the entire codebase and external sources,  
**So that** my contributions are informed and evidence-based.

**Acceptance Criteria:**
- [ ] Agent can query CASS for historical patterns
- [ ] Agent can search codebase using TLDR
- [ ] Agent can fetch external documentation/APIs
- [ ] Research findings shared with facilitator only (not broadcast)

### Story 4: Isolated Context
**As a** non-participating agent,  **I want** consensus discussions to not pollute my context,  **So that** I can focus on my own tasks efficiently.

**Acceptance Criteria:**
- [ ] Discussion messages use directed MCP messaging (not broadcast)
- [ ] Only participants receive discussion messages
- [ ] Discussion ID scopes all messages
- [ ] No impact on other agents' performance

### Story 5: Persistent Knowledge
**As a** future developer,  **I want** to query past consensus decisions,  **So that** I understand why certain architectural choices were made.

**Acceptance Criteria:**
- [ ] Full conversation transcript stored in CASS
- [ ] Consensus recommendation stored in bead metadata
- [ ] Queryable via CASS pattern matching
- [ ] Linked to originating bead for traceability

---

## 3. Functional Requirements

### 3.1 Bead Structure Extensions

#### FR-1: Consensus Configuration Field
**Priority:** Must Have  
**Description:** Extend Bead struct with consensus configuration

```go
// ConsensusConfig defines multi-agent discussion parameters
type ConsensusConfig struct {
    Required      bool              `json:"required" yaml:"required"`
    Topic         string            `json:"topic" yaml:"topic"`
    Description   string            `json:"description" yaml:"description"`
    ParticipantCount int           `json:"participant_count" yaml:"participant_count"` // 2-4
    ResearchScope ResearchScope     `json:"research_scope" yaml:"research_scope"`
    MaxRounds     int               `json:"max_rounds" yaml:"max_rounds"` // default: 4
    Timeout       time.Duration     `json:"timeout" yaml:"timeout"` // default: 30m
}

type ResearchScope struct {
    CassPatterns  bool `json:"cass_patterns" yaml:"cass_patterns"`
    Codebase      bool `json:"codebase" yaml:"codebase"`
    Dependencies  bool `json:"dependencies" yaml:"dependencies"`
    External      bool `json:"external" yaml:"external"` // APIs, docs
}
```

**Acceptance Criteria:**
- [ ] ConsensusConfig added to Bead struct
- [ ] Configuration parsed from PRD YAML
- [ ] Backward compatible (optional field)
- [ ] Validation: participant_count between 2-4
- [ ] Validation: max_rounds between 2-6

### 3.2 Agent Type Extensions

#### FR-2: Consensus Facilitator Agent Type
**Priority:** Must Have  
**Description:** New specialized agent for discussion moderation

```go
const (
    // ... existing types
    AgentConsensusFacilitator AgentType = "consensus-facilitator"
    AgentConsensusExpert      AgentType = "consensus-expert"
)
```

**Acceptance Criteria:**
- [ ] AgentConsensusFacilitator type added
- [ ] AgentConsensusExpert type added
- [ ] Facilitator has special permissions to manage discussion flow
- [ ] Facilitator can spawn expert agents dynamically

### 3.3 MCP Message Extensions

#### FR-3: Discussion Message Types
**Priority:** Must Have  
**Description:** New MCP message types for isolated discussion

```go
const (
    // Existing types...
    
    // Consensus discussion messages
    MsgTypeDiscussionOpen      = "discussion_open"
    MsgTypeDiscussionResearch  = "discussion_research"
    MsgTypeDiscussionMessage   = "discussion_message"
    MsgTypeDiscussionConsensus = "discussion_consensus"
    MsgTypeDiscussionComplete  = "discussion_complete"
    MsgTypeDiscussionTimeout   = "discussion_timeout"
)

// DiscussionMessagePayload for all discussion messages
type DiscussionMessagePayload struct {
    DiscussionID string                 `json:"discussion_id"`
    BeadID       string                 `json:"bead_id"`
    Round        int                    `json:"round"`
    AgentID      string                 `json:"agent_id"`
    AgentRole    string                 `json:"agent_role"` // facilitator, expert
    Content      string                 `json:"content"`
    Research     *ResearchPayload       `json:"research,omitempty"`
    Metadata     map[string]interface{} `json:"metadata,omitempty"`
    Timestamp    time.Time              `json:"timestamp"`
}

type ResearchPayload struct {
    Sources      []string `json:"sources"`
    KeyFindings  []string `json:"key_findings"`
    RelevantFiles []string `json:"relevant_files,omitempty"`
    ExternalData []string `json:"external_data,omitempty"`
}
```

**Acceptance Criteria:**
- [ ] All message types implemented
- [ ] Messages include discussion_id for scoping
- [ ] Facilitator receives all messages
- [ ] Expert-to-expert communication routed through facilitator
- [ ] Messages NOT broadcast to non-participants

### 3.4 Discussion Orchestration

#### FR-4: Discussion Lifecycle
**Priority:** Must Have  
**Description:** Complete flow from trigger to completion

```
┌─────────────────────────────────────────────────────────────┐
│  DISCUSSION LIFECYCLE                                        │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  TRIGGER                                                     │
│  └─> Bead with consensus:required becomes ready            │
│      └─> Overseer spawns Facilitator                       │
│                                                              │
│  INITIALIZE                                                  │
│  └─> Facilitator creates discussion_id                     │
│      └─> Facilitator spawns N expert agents                │
│      └─> Facilitator broadcasts "discussion_open"          │
│          Payload: topic, context, research_tasks           │
│                                                              │
│  RESEARCH PHASE (async, parallel)                           │
│  └─> Each expert:                                          │
│      • Query CASS for relevant patterns                    │
│      • TLDR analysis of codebase                           │
│      • External source research (if enabled)               │
│      └─> Send "discussion_research" → Facilitator only    │
│                                                              │
│  DISCUSSION ROUNDS                                          │
│  For round 1 to max_rounds:                                 │
│  └─> Facilitator assigns turns based on research          │
│      └─> Expert A → Facilitator: Position + reasoning      │
│      └─> Expert B → Facilitator: Response/Questions        │
│      └─> Expert C → Facilitator: Synthesis/Concerns        │
│      └─> Facilitator analyzes for convergence              │
│          If converged: GOTO CONSENSUS                      │
│                                                              │
│  CONSENSUS                                                  │
│  └─> Facilitator synthesizes positions                     │
│      └─> Facilitator broadcasts "discussion_consensus"     │
│      Payload: {                                             │
│          recommendation: "Recommended approach...",       │
│          reasoning: "Based on...",                        │
│          risks: [{"risk": "...", "mitigation": "..."}],   │
│          alternatives: [{"option": "...", "pros": [],     │
│                          "cons": []}],                    │
│          confidence: 0.85,                                 │
│          dissenting_views: ["..."]                        │
│      }                                                      │
│                                                              │
│  PERSISTENCE                                                │
│  └─> Facilitator saves to CASS:                           │
│      Key: "consensus://<bead-id>"                         │
│      Value: full transcript + consensus                    │
│      └─> Update bead metadata:                            │
│      "consensus_recommendation": <summary>                │
│      "consensus_confidence": 0.85                         │
│      "consensus_participants": [agent-ids]                │
│                                                              │
│  COMPLETION                                                 │
│  └─> Facilitator broadcasts "discussion_complete"         │
│      └─> Facilitator closes bead                          │
│      └─> Expert agents terminate                          │
└─────────────────────────────────────────────────────────────┘
```

**Acceptance Criteria:**
- [ ] Complete lifecycle implemented
- [ ] Each phase has defined timeout
- [ ] Facilitator manages all state transitions
- [ ] Experts cannot proceed without facilitator
- [ ] Research phase runs in parallel
- [ ] Discussion phase is turn-based

#### FR-5: Convergence Detection
**Priority:** Should Have  
**Description:** Facilitator automatically detects when consensus is reached

```go
type ConvergenceAnalysis struct {
    AgreementScore    float64  `json:"agreement_score"`    // 0.0-1.0
    KeyAgreements     []string `json:"key_agreements"`
    KeyDisagreements  []string `json:"key_disagreements"`
    Converged         bool     `json:"converged"`
    RecommendedAction string   `json:"recommended_action"` // continue, converge, escalate
}

func (f *Facilitator) AnalyzeConvergence(messages []DiscussionMessage) ConvergenceAnalysis {
    // Analyze semantic similarity of positions
    // Check for explicit agreement statements
    // Look for convergence keywords ("agree", "concur", "same page")
    // Return convergence status and recommendation
}
```

**Acceptance Criteria:**
- [ ] Semantic analysis of agent positions
- [ ] Detection of agreement patterns
- [ ] Configurable convergence threshold
- [ ] Escalation if max rounds reached without consensus
- [ ] Dissenting views captured in final output

### 3.5 Research Capabilities

#### FR-6: Research Tools for Experts
**Priority:** Must Have  
**Description:** Comprehensive research during research phase

```go
// ResearchToolSet available to expert agents
type ResearchToolSet interface {
    // CASS Pattern Search
    QueryCassPatterns(query string, context map[string]string) ([]Pattern, error)
    
    // Codebase Analysis
    SearchCodebase(pattern string) ([]FileMatch, error)
    AnalyzeDependencies(file string) ([]Dependency, error)
    
    // External Sources
    FetchDocumentation(url string) (string, error)
    QueryAPI(endpoint string, params map[string]string) (interface{}, error)
    
    // Context Gathering
    GetBeadHistory(beadID string) ([]BeadEvent, error)
    GetRelatedBeads(beadID string) ([]Bead, error)
}
```

**Acceptance Criteria:**
- [ ] All research tools implemented
- [ ] Research results attached to discussion_research message
- [ ] Sources cited in expert positions
- [ ] Research timeout enforced (5-10 min)
- [ ] Failed research doesn't block discussion (graceful degradation)

### 3.6 Knowledge Persistence

#### FR-7: CASS Storage Format
**Priority:** Must Have  
**Description:** Store consensus as searchable CASS pattern

```go
// ConsensusPattern stored in CASS
type ConsensusPattern struct {
    PatternID        string                 `json:"pattern_id"`
    Type             string                 `json:"type"` // "consensus"
    BeadID           string                 `json:"bead_id"`
    Topic            string                 `json:"topic"`
    Timestamp        time.Time              `json:"timestamp"`
    Participants     []string               `json:"participants"`
    
    // Full Discussion
    Transcript       []DiscussionMessage    `json:"transcript"`
    
    // Consensus Output
    Recommendation   ConsensusRecommendation `json:"recommendation"`
    
    // Searchable Metadata
    Keywords         []string               `json:"keywords"`
    Technologies     []string               `json:"technologies"`
    DecisionFactors  []string               `json:"decision_factors"`
}

type ConsensusRecommendation struct {
    Summary          string                 `json:"summary"`
    DetailedReasoning string                `json:"detailed_reasoning"`
    Risks            []Risk                 `json:"risks"`
    Alternatives     []Alternative          `json:"alternatives"`
    Confidence       float64                `json:"confidence"`
    DissentingViews  []string               `json:"dissenting_views"`
    ActionItems      []ActionItem           `json:"action_items"`
}
```

**Acceptance Criteria:**
- [ ] Pattern stored in CASS with key "consensus://<bead-id>"
- [ ] Full transcript preserved
- [ ] Searchable metadata extracted
- [ ] Linked to originating bead
- [ ] Queryable by topic, technologies, keywords

#### FR-8: Bead Metadata Updates
**Priority:** Must Have  
**Description:** Update bead with consensus summary

```yaml
# Bead metadata after consensus
metadata:
  consensus_recommendation: "Use Kafka for event streaming due to..."
  consensus_confidence: "0.92"
  consensus_participants: "facilitator-abc,expert-def,expert-ghi"
  consensus_duration: "18m30s"
  consensus_rounds: "3"
  consensus_pattern_id: "consensus://myproject-xyz"
```

**Acceptance Criteria:**
- [ ] Bead metadata updated upon consensus completion
- [ ] Human-readable summary included
- [ ] Links to full CASS pattern
- [ ] Statistics captured (duration, rounds, participants)
- [ ] Backward compatible (beads without consensus work normally)

---

## 4. Non-Functional Requirements

### 4.1 Performance

#### NFR-1: Discussion Latency
**Priority:** Should Have  
- Research phase: Complete within 10 minutes
- Discussion rounds: Maximum 2 minutes per round
- Total session: Complete within 30 minutes (configurable)

#### NFR-2: Scalability
**Priority:** Should Have  
- Multiple consensus sessions can run in parallel
- No impact on non-consensus bead processing
- MCP server handles 50+ concurrent discussion channels

#### NFR-3: Resource Usage
**Priority:** Could Have  
- Each expert agent: 1-2GB memory
- Facilitator agent: 512MB memory
- Disk: Store only compressed transcripts in CASS

### 4.2 Reliability

#### NFR-4: Fault Tolerance
**Priority:** Must Have  
- If expert fails, facilitator continues with remaining experts
- If facilitator fails, session marked as failed, bead reopened
- Research failures don't block discussion (proceed without)
- Timeout handling at each phase

#### NFR-5: State Recovery
**Priority:** Should Have  
- Discussion state persisted every round
- Can resume from last checkpoint if interrupted
- Idempotent operations (restarting doesn't duplicate)

### 4.3 Security

#### NFR-6: Message Isolation
**Priority:** Must Have  
- Discussion messages only visible to participants
- No logging of discussion content to system logs
- Access control on CASS consensus patterns

#### NFR-7: External Research Safety
**Priority:** Must Have  
- External API calls through sandbox/proxy
- Rate limiting on external queries
- No credentials exposed to expert agents

---

## 5. Technical Considerations

### 5.1 Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                    HYDRA OVERSEER                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Consensus Scheduler                                 │  │
│  │  • Detects beads with consensus:required             │  │
│  │  • Spawns Facilitator instead of normal agent        │  │
│  └──────────────────┬───────────────────────────────────┘  │
└─────────────────────┼────────────────────────────────────────┘
                      │
                      ▼
┌──────────────────────────────────────────────────────────────┐
│              CONSENSUS FACILITATOR AGENT                     │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Discussion Manager                                  │  │
│  │  • Creates discussion_id                             │  │
│  │  • Spawns N Expert agents                            │  │
│  │  • Manages discussion state machine                  │  │
│  │  • Detects convergence                               │  │
│  └──────────────────┬───────────────────────────────────┘  │
│                     │ MCP (directed messages)               │
│  ┌──────────────────┴───────────────────────────────────┐  │
│  │              MCP Message Router                      │  │
│  │  • Routes by discussion_id + To field               │  │
│  │  • Filters: Only participants receive messages       │  │
│  └──────────────────┬───────────────────────────────────┘  │
└─────────────────────┼────────────────────────────────────────┘
                      │
        ┌─────────────┼─────────────┐
        ▼             ▼             ▼
┌──────────────┐ ┌──────────┐ ┌──────────────┐
│  Expert A    │ │Expert B  │ │  Expert C    │
│  (Research + │ │(Research +│ │  (Research + │
│   Discuss)   │ │ Discuss) │ │   Discuss)   │
└──────────────┘ └──────────┘ └──────────────┘
        │             │             │
        └─────────────┴─────────────┘
                      │
                      ▼
┌──────────────────────────────────────────────────────────────┐
│  KNOWLEDGE LAYER                                             │
│  ┌─────────────────────┐    ┌──────────────────────────────┐│
│  │  CASS Memory        │    │  Bead Metadata               ││
│  │  consensus://id     │    │  consensus_recommendation    ││
│  │  Full transcript    │    │  consensus_confidence        ││
│  │  Searchable         │    │  consensus_pattern_id        ││
│  └─────────────────────┘    └──────────────────────────────┘│
└──────────────────────────────────────────────────────────────┘
```

### 5.2 Component Changes

#### 5.2.1 pkg/types/bead.go
**Changes:**
- Add `ConsensusConfig` struct
- Add `ConsensusConfig` field to `Bead` struct
- Add `AgentConsensusFacilitator` and `AgentConsensusExpert` types

#### 5.2.2 internal/mcp/messages.go
**Changes:**
- Add discussion message type constants
- Add `DiscussionMessagePayload` struct
- Add `ResearchPayload` struct

#### 5.2.3 internal/core/overseer.go
**Changes:**
- Modify bead processing logic to detect `consensus:required`
- Instead of spawning normal agent, spawn Facilitator
- Pass consensus config to Facilitator via environment

#### 5.2.4 New: cmd/consensus-facilitator/main.go
**New component:** Facilitator agent binary
- Manages discussion lifecycle
- Spawns expert agents
- Coordinates MCP messages
- Persists results

#### 5.2.5 New: cmd/consensus-expert/main.go
**New component:** Expert agent binary
- Executes research phase
- Participates in discussion rounds
- Reports to facilitator

#### 5.2.6 New: internal/consensus/
**New package:** Consensus logic
- `facilitator.go`: Facilitator implementation
- `expert.go`: Expert agent implementation
- `discussion.go`: Discussion state machine
- `convergence.go`: Convergence detection algorithms
- `persistence.go`: CASS and metadata storage

### 5.3 MCP Routing Strategy

**Message Routing Rules:**
1. All discussion messages include `discussion_id` in payload
2. Facilitator sets `To` field to route to specific expert
3. MCP server maintains `discussion_id → participants` mapping
4. Messages without matching `discussion_id` in participant list are dropped
5. Facilitator can broadcast to all participants by omitting `To` field

**Example Message Flow:**
```
Facilitator → MCP: discussion_open, To: "", Payload: {discussion_id: "disc-123", ...}
MCP routes to: [expert-1, expert-2, expert-3] (all participants)

Expert-1 → MCP: discussion_research, To: "facilitator-abc", Payload: {...}
MCP routes to: [facilitator-abc] only

Facilitator → MCP: discussion_message, To: "expert-1", Payload: {...}
MCP routes to: [expert-1] only
```

### 5.4 Configuration Schema

```yaml
# Example PRD with consensus configuration
beads:
  - id: architecture-decision-001
    title: Choose Message Queue Technology
    type: architecture
    description: Evaluate Kafka vs RabbitMQ vs NATS
    
    # Consensus configuration - NEW
    consensus:
      required: true
      topic: "Message Queue Selection for Event Streaming"
      description: |
        We need to choose a message queue technology for our event-driven 
        architecture. Consider: throughput, latency, operational complexity, 
        and team expertise.
      participant_count: 3
      research_scope:
        cass_patterns: true      # Search historical patterns
        codebase: true           # Analyze existing code
        dependencies: true       # Check dependency impact
        external: true           # Fetch external docs
      max_rounds: 4
      timeout: 30m
    
    dependencies:
      - requirements-analysis
```

---

## 6. Risks and Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Context leakage to non-participants | Low | High | Strict MCP routing by discussion_id + participant list |
| Endless discussion loops | Medium | Medium | Max rounds limit + convergence detection + timeout |
| Low quality consensus (groupthink) | Medium | High | Diverse expert specializations + dissent capture |
| Research phase too slow | Medium | Medium | Parallel execution + timeout + graceful degradation |
| Facilitator single point of failure | Low | High | Checkpoint persistence + recovery logic |
| Storage bloat from transcripts | Medium | Low | Compression + archival policy + CASS limits |
| Complex configuration burden | Medium | Medium | Sensible defaults + validation + examples |
| Consensus not actionable | Medium | High | Structured output format + action items required |

---

## 7. MoSCoW Prioritization

### Must Have
- [ ] ConsensusConfig in Bead structure
- [ ] ConsensusFacilitator and ConsensusExpert agent types
- [ ] MCP discussion message types with isolation
- [ ] Complete discussion lifecycle (init → research → discuss → consensus → persist)
- [ ] CASS storage of full transcript
- [ ] Bead metadata update with recommendation
- [ ] Research tools (CASS, codebase)
- [ ] Max rounds and timeout enforcement

### Should Have
- [ ] Convergence detection algorithm
- [ ] External source research (APIs, docs)
- [ ] Discussion checkpoint/resume
- [ ] Confidence scoring
- [ ] Risk and alternative capture
- [ ] Query interface for historical consensus

### Could Have
- [ ] Visual consensus timeline in UI
- [ ] Real-time discussion streaming
- [ ] Human intervention/joining
- [ ] Consensus templates (predefined patterns)
- [ ] Automatic follow-up bead creation

### Won't Have (in v1)
- [ ] Real-time human participation
- [ ] Voice/video discussion
- [ ] Multi-language discussions
- [ ] Consensus across multiple beads simultaneously

---

## 8. Success Metrics & KPIs

### Adoption Metrics
- **Consensus Adoption Rate**: % of eligible beads using consensus
- **Average Participants**: 2-4 per session (target: 3)
- **Configuration Success**: 95%+ of consensus configs valid

### Quality Metrics
- **Consensus Reach Rate**: 90%+ sessions reach conclusion
- **Average Rounds**: 2-4 rounds (target: 3)
- **Confidence Score**: Average >0.8
- **Dissent Capture**: 100% of dissenting views recorded

### Performance Metrics
- **Research Phase**: <10 minutes
- **Discussion Phase**: <20 minutes
- **Total Session**: <30 minutes
- **Context Isolation**: 0 incidents of leakage

### Outcome Metrics
- **Decision Quality**: 50%+ reduction in post-implementation issues
- **Knowledge Reuse**: Historical consensus referenced in 20%+ of related beads
- **Developer Satisfaction**: Positive feedback on decision traceability

---

## 9. Open Questions

1. **Expert Specialization**: Should experts have different specializations (security, performance, architecture) or be homogeneous?

2. **Facilitator Authority**: Should facilitator have authority to override/consult human if consensus cannot be reached?

3. **Escalation Path**: What happens when max rounds reached without consensus? Close bead as "partial-consensus" or escalate?

4. **Notification Strategy**: Should stakeholders be notified when consensus completes?

5. **Review Process**: Should consensus recommendations require human approval before bead closes?

---

## 10. Implementation Phases

### Phase 1: Core Infrastructure (Week 1-2)
- Add ConsensusConfig to Bead struct
- Add new agent types
- Create MCP message types
- Basic Facilitator scaffolding

### Phase 2: Discussion Lifecycle (Week 3-4)
- Facilitator spawns experts
- Research phase with CASS/codebase
- Turn-based discussion rounds
- State machine management

### Phase 3: Consensus & Persistence (Week 5-6)
- Convergence detection
- Recommendation synthesis
- CASS storage
- Bead metadata updates
- Close bead flow

### Phase 4: Polish & Testing (Week 7-8)
- Timeout handling
- Fault tolerance
- Configuration validation
- Documentation and examples
- Integration tests

---

## 11. Research Insights

### Codebase Patterns
- MCP already supports directed messaging via `To` field
- File reservation system demonstrates conflict-free coordination
- CASS memory provides pattern storage and retrieval
- Worker spawning pattern can be extended for multi-agent

### Best Practices from Research
- **Anthropic Recommendation**: Use simple, composable patterns rather than complex frameworks
- **Facilitated Discussion**: More effective than open chat for reaching conclusions
- **Research-First**: Experts with prior research contribute higher quality
- **Structured Output**: Conclusions need clear format (recommendation, risks, alternatives)

### Domain Knowledge
- Multi-agent consensus systems exist in:
  - **AutoGen** (Microsoft): Group chat with speaker selection
  - **Evaluator-Optimizer**: Iterative refinement pattern
  - **Voting**: Parallel independent analysis with aggregation
- This design combines facilitated discussion + research-first + structured output

---

## Appendix A: Example Consensus Session

### Scenario: Database Selection for New Service

**PRD Configuration:**
```yaml
consensus:
  required: true
  topic: "Database Selection for User Analytics Service"
  participant_count: 3
  research_scope:
    cass_patterns: true
    codebase: true
    dependencies: true
    external: true
  max_rounds: 4
  timeout: 30m
```

**Session Flow:**

**Round 0 (Research):**
- Expert-1 (Data): Researches query patterns, finds CASS pattern for time-series
- Expert-2 (Ops): Analyzes operational overhead, queries external monitoring docs
- Expert-3 (Perf): Profiles latency requirements, searches codebase for similar services

**Round 1 (Opening Positions):**
- Expert-1: "TimescaleDB - purpose-built for time-series, matches our query patterns"
- Expert-2: "PostgreSQL with partitioning - team expertise, lower ops overhead"
- Expert-3: "ClickHouse - best write throughput, matches performance requirements"

**Round 2 (Responses):**
- Expert-2: "Concerned about ClickHouse operational complexity - no team experience"
- Expert-3: "Agree on ops concern. PostgreSQL partitioning might not scale to our volume"
- Expert-1: "TimescaleDB is PostgreSQL-compatible - balances both concerns"

**Round 3 (Refinement):**
- Expert-2: "TimescaleDB seems like good middle ground. What's migration path?"
- Expert-3: "Can use hypertables incrementally - low migration risk"
- Expert-1: "Existing PostgreSQL tooling works - minimal ops training needed"

**Convergence Detected:** All experts converging on TimescaleDB

**Consensus Output:**
```json
{
  "recommendation": "Use TimescaleDB for User Analytics Service",
  "reasoning": "Balances query performance, write throughput, and operational complexity. PostgreSQL-compatible tooling minimizes ops overhead.",
  "risks": [
    {
      "risk": "Team has no TimescaleDB experience",
      "mitigation": "Start with hypertables only, use standard PostgreSQL for everything else"
    },
    {
      "risk": "Migration complexity",
      "mitigation": "New service - no migration needed. Use from day 1."
    }
  ],
  "alternatives": [
    {
      "option": "ClickHouse",
      "pros": ["Best write throughput"],
      "cons": ["High operational complexity", "No team experience"]
    },
    {
      "option": "PostgreSQL with manual partitioning",
      "pros": ["Team expertise", "Low ops overhead"],
      "cons": ["May not scale to projected volume"]
    }
  ],
  "confidence": 0.88,
  "dissenting_views": [],
  "action_items": [
    "Set up TimescaleDB instance in dev environment",
    "Create proof-of-concept with sample data",
    "Document query patterns in CASS"
  ]
}
```

**Bead Metadata Updated:**
```yaml
consensus_recommendation: "Use TimescaleDB for User Analytics Service"
consensus_confidence: "0.88"
consensus_participants: "facilitator-xyz,data-expert-abc,ops-expert-def,perf-expert-ghi"
consensus_pattern_id: "consensus://myproject-ua-db-selection"
```

**CASS Pattern Stored:**
```
Key: consensus://myproject-ua-db-selection
Content: Full transcript + recommendation + metadata
Searchable by: "TimescaleDB", "time-series", "database selection"
```

---

*End of PRD*
