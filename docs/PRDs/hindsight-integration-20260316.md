# Product Requirements Document: Hindsight Memory System Integration

**Date:** March 16, 2026  
**Version:** 1.0  
**Status:** Draft - Ready for Review  
**Author:** Hydra Development Team  
**Feature Branch:** `feature/hindsight-integration`

---

## 1. Executive Summary

### 1.1 Overview

This PRD specifies the integration of **Hindsight** (vectorize-io/hindsight) into the Hydra orchestration system to replace the existing `cass_memory` institutional knowledge store. Hindsight is a state-of-the-art agent memory system that enables AI agents to learn over time through biomimetic data structures (world facts, experiences, mental models).

### 1.2 Business Value

**Current Pain Points:**
- Project-isolated learning with `cass_memory` prevents organizational knowledge sharing
- No cross-project pattern reuse (agents solve identical problems across projects)
- Limited scalability (current system struggles beyond 100 beads/day)
- Siloed knowledge that doesn't compound over time

**Expected Improvements:**
- **≥40% reduction** in time-to-completion for similar tasks across projects
- **≥30% cross-project pattern reuse rate** through shared memory banks
- **200-500 beads/day capacity** supporting 10 developers
- **Compounding institutional intelligence** that improves with every solved problem

### 1.3 Success Metrics

| Metric | Current (cass_memory) | Target (Hindsight) | Measurement |
|--------|----------------------|-------------------|-------------|
| Pattern reuse | 0% cross-project | ≥30% cross-project | Pattern application tracking |
| Time-to-completion | Baseline | -40% | Execution duration comparison |
| Daily bead throughput | ~100/day | 200-500/day | Bead completion rate |
| Knowledge retention | Per-project | Organization-wide | Cross-project query success |
| Developer productivity | Baseline | +25% | Tasks completed per day |

### 1.4 Scope

**In Scope:**
- Complete replacement of `cass_memory` with Hindsight
- Embedded Hindsight server lifecycle management
- Four deterministic integration points (recall, retain, reflect, quality scoring)
- Cross-project memory sharing with unified "hydra" bank
- Fail-fast error handling (mandatory dependency)

**Out of Scope:**
- Hindsight skills for Pi agents (Phase 2 enhancement)
- UI/dashboard for memory management
- Advanced analytics and reporting
- Multi-tenant isolation (single organization)

---

## 2. User Stories

### US-001: Pre-Execution Context Retrieval
> As a Hydra overseer, I want to retrieve relevant historical memories before spawning a worker, so that the worker has context from similar past executions.

**Acceptance Criteria:**
- Overseer queries Hindsight with bead title and type before worker spawn
- System retrieves top 5 most relevant memories (similarity ≥0.75)
- Context is injected into worker environment variables
- If recall fails, worker spawns without context (graceful degradation for recall)

### US-002: Post-Execution Memory Retention
> As a Hydra worker, I want to store execution results in Hindsight after completing a bead, so that future similar tasks can learn from this execution.

**Acceptance Criteria:**
- Worker calls `hindsight.Retain()` with structured execution memory
- Memory includes: bead metadata, success/failure, output, duration, files
- If retention fails, worker reports failure and triggers retry
- Success logged: `[HINDSIGHT] Retained execution memory for bead X`

### US-003: Pattern Quality Enhancement
> As a pattern reviewer, I want Hindsight to provide historical success rates for similar patterns, so that quality scoring is evidence-based.

**Acceptance Criteria:**
- Pattern review queries Hindsight for similar patterns
- Quality score adjusted based on historical success rate
- Formula: `adjusted_score = base_score * 0.7 + (success_rate * 0.3)`
- Patterns with high historical success get quality score boost

### US-004: Settled-State Reflection
> As a Hydra system, I want to perform reflection on settled beads, so that insights and mental models are generated from successful executions.

**Acceptance Criteria:**
- Reflection triggered after bead settlement (2-minute delay)
- Hindsight `Reflect()` called with bead ID and context
- Insights stored as new memories with type "reflection_insight"
- Logging: `[HINDSIGHT] Generated N insights from reflection`

### US-005: Cross-Project Knowledge Sharing
> As a developer, I want patterns learned in Project A automatically available in Project B, so I don't solve the same problems repeatedly.

**Acceptance Criteria:**
- Single "hydra" memory bank shared across all projects
- Metadata includes project attribution for filtering
- Query can filter by project when needed
- Cross-project patterns available within 24 hours of extraction

### US-006: Context-Processor Intelligence Analysis
> As a Hydra overseer, I want a context-processor agent to analyze beads before execution using Hindsight, so that each bead gets optimal routing and enriched context.

**Acceptance Criteria:**
- Context-processor runs before every bead execution
- Queries Hindsight for patterns matching bead description
- Decides routing strategy: direct execution, specialist routing, or consensus
- Enriches prompt with relevant patterns and anti-patterns from Hindsight
- Stores analysis results in Hindsight with metadata

### US-007: Multi-Agent Consensus with Hindsight
> As an architect, I want complex beads to trigger multi-agent consensus with decisions stored in Hindsight, so that architectural decisions are well-vetted and learnable.

**Acceptance Criteria:**
- Context-processor triggers consensus for complex beads (>5 files or architectural changes)
- Facilitator agent spawns 2-4 expert reviewers
- Reviewers analyze independently using Hindsight context
- Vote to approve, reject, or request changes
- Store consensus results in Hindsight for future reference

---

## 3. Functional Requirements

### FR-001a: Embedded Hindsight Server Lifecycle Management (Part 1/2)
Initialize and manage the Hindsight embedded Python server as a mandatory dependency of Hydra.

**Acceptance Criteria:**
- Server starts automatically when Hydra initializes
- Health check validates server is ready before accepting beads
- Server stops gracefully on Hydra shutdown

### FR-001b: Embedded Hindsight Server Lifecycle Management (Part 2/2)
Initialize and manage the Hindsight embedded Python server as a mandatory dependency of Hydra.

**Acceptance Criteria:**
- If server fails to start, Hydra exits with error (fail-fast)
- Server runs on configurable port (default: 8888)

### FR-002a: Hindsight Go Client Implementation (Part 1/2)
Create a Go HTTP client to communicate with Hindsight REST API for all memory operations.

**Acceptance Criteria:**
- Client supports `Recall()`, `Retain()`, and `Reflect()` operations
- Client handles connection pooling and retry logic
- Client includes timeout configuration (default: 30s)

### FR-002b: Hindsight Go Client Implementation (Part 2/2)
Create a Go HTTP client to communicate with Hindsight REST API for all memory operations.

**Acceptance Criteria:**
- Client provides health check method
- Errors include detailed context for debugging

### FR-003a: Configuration System Integration (Part 1/2)
Add Hindsight configuration to existing Hydra configuration system with validation.

**Acceptance Criteria:**
- Configuration added to `IntelligenceConfig` struct
- Support YAML configuration in `hydra.yaml`
- Environment variable overrides available

### FR-003b: Configuration System Integration (Part 2/2)
Add Hindsight configuration to existing Hydra configuration system with validation.

**Acceptance Criteria:**
- Configuration validation at startup
- Sensible defaults provided for all options

### FR-004a: Pre-Execution Context Retrieval Integration (Part 1/2)
Integrate Hindsight `Recall()` before worker spawning to provide historical context.

**Acceptance Criteria:**
- Overseer calls `Recall()` in `spawnWorkerInternal()` before worker creation
- Query includes bead title, type, and target files
- Results limited to 5 memories with similarity ≥0.75

### FR-004b: Pre-Execution Context Retrieval Integration (Part 2/2)
Integrate Hindsight `Recall()` before worker spawning to provide historical context.

**Acceptance Criteria:**
- Context serialized to JSON and added to worker environment
- Duration logged: `[HINDSIGHT] Recalled N memories for bead X`

### FR-005a: Worker Context Parsing (Part 1/2)
Parse Hindsight context from environment variables in the worker process.

**Acceptance Criteria:**
- Worker reads `HYDRA_HISTORIESIGHT_CONTEXT` environment variable
- Context deserialized from JSON format
- Parsed context logged at worker startup

### FR-005b: Worker Context Parsing (Part 2/2)
Parse Hindsight context from environment variables in the worker process.

**Acceptance Criteria:**
- Graceful handling if context is missing or malformed

### FR-006a: Post-Execution Memory Retention (Part 1/2)
Replace `cass_memory.AddPattern()` with Hindsight `Retain()` in worker completion flow.

**Acceptance Criteria:**
- `recordExecutionResult()` calls `hindsight.Retain()` instead of `cass_memory`
- Memory includes structured data: bead ID, title, type, success, output, error, files, duration
- Metadata includes timestamp, project, agent type

### FR-006b: Post-Execution Memory Retention (Part 2/2)
Replace `cass_memory.AddPattern()` with Hindsight `Retain()` in worker completion flow.

**Acceptance Criteria:**
- If retention fails, result.Success set to false and error recorded
- Worker retry triggered on retention failure

### FR-007a: Mandatory Retention Error Handling (Part 1/2)
Implement fail-fast behavior when Hindsight retention fails.

**Acceptance Criteria:**
- Retention errors logged: `[ERROR] Hindsight retention failed: <error>`
- Bead marked as failed when retention fails
- Retry mechanism triggered according to configured policy

### FR-007b: Mandatory Retention Error Handling (Part 2/2)
Implement fail-fast behavior when Hindsight retention fails.

**Acceptance Criteria:**
- No silent failures or data loss
- Error propagated to overseer for visibility

### FR-008a: Pattern Quality Scoring Enhancement (Part 1/2)
Enhance pattern quality scoring using Hindsight historical success data.

**Acceptance Criteria:**
- `calculateQualityScore()` queries Hindsight for similar patterns
- Historical success rate retrieved for pattern type
- Quality score formula: `adjusted = base * 0.7 + success_rate * 0.3`

### FR-008b: Pattern Quality Scoring Enhancement (Part 2/2)
Enhance pattern quality scoring using Hindsight historical success data.

**Acceptance Criteria:**
- Minimum 3 similar patterns required for adjustment
- Scores capped at 1.0 maximum

### FR-009a: Settled-State Reflection Integration (Part 1/2)
Add Hindsight `Reflect()` call after bead settlement detection.

**Acceptance Criteria:**
- Reflection triggered in `settleBead()` after immutability confirmed
- `Reflect()` called with bead ID and execution context
- Generated insights stored as new memories

### FR-009b: Settled-State Reflection Integration (Part 2/2)
Add Hindsight `Reflect()` call after bead settlement detection.

**Acceptance Criteria:**
- Each insight linked to source bead via metadata
- Non-blocking execution (reflection failure doesn't fail settlement)

### FR-010a: Cross-Project Memory Bank (Part 1/2)
Configure single memory bank shared across all Hydra projects.

**Acceptance Criteria:**
- Bank ID: "hydra" used for all operations
- Metadata includes project attribution for every memory
- Query results include project information

### FR-010b: Cross-Project Memory Bank (Part 2/2)
Configure single memory bank shared across all Hydra projects.

**Acceptance Criteria:**
- Ability to filter queries by project when needed
- No project isolation (intentional for knowledge sharing)

### FR-011a: LLM Provider Abstraction (Part 1/2)
Support multiple LLM providers through configuration with modular API key extraction.

**Acceptance Criteria:**
- Support OpenAI-compatible APIs (base_url configuration)
- Support native MiniMax integration
- API key retrieved via modular extraction function (reuses existing `KIMI_API_KEY` pattern)

### FR-011b: LLM Provider Abstraction (Part 2/2)
Support multiple LLM providers through configuration with modular API key extraction.

**Acceptance Criteria:**
- Environment variable name configurable via `llm_api_key_env` setting
- API key logged with length only (never expose key value)

### FR-012a: SQLite Storage Backend (Part 1/2)
Use SQLite for embedded Hindsight storage in local development.

**Acceptance Criteria:**
- SQLite database stored at configurable path (default: ~/.hydra/hindsight.db)
- Automatic schema migration on startup
- pgvector extension not required for SQLite

### FR-012b: SQLite Storage Backend (Part 2/2)
Use SQLite for embedded Hindsight storage in local development.

**Acceptance Criteria:**
- Database file permissions set to 0600
- Backup/restore utilities documented

### FR-013a: Observability and Logging (Part 1/2)
Implement comprehensive logging for Hindsight operations.

**Acceptance Criteria:**
- Server lifecycle events logged (start, stop, health check)
- All memory operations logged with duration and result count
- Error logs include detailed context and stack traces

### FR-013b: Observability and Logging (Part 2/2)
Implement comprehensive logging for Hindsight operations.

**Acceptance Criteria:**
- Log format: `[HINDSIGHT] <operation> <bead_id> <result>`
- No sensitive data (API keys, secrets) logged

### FR-014a: cass_memory Deprecation and Removal (Part 1/2)
Remove all cass_memory dependencies and code.

**Acceptance Criteria:**
- `internal/intelligence/cass_memory.go` deleted
- All cass_memory imports removed from codebase
- cass_memory client initialization removed from overseer

### FR-014b: cass_memory Deprecation and Removal (Part 2/2)
Remove all cass_memory dependencies and code.

**Acceptance Criteria:**
- cass_memory calls replaced with Hindsight equivalents
- Documentation updated to remove cass_memory references

### FR-015a: Context-Processor Agent Integration (Part 1/2)
Create dedicated context-processor agent that runs before each bead execution.

**Acceptance Criteria:**
- New agent type: context-processor
- Agent defined in .agents/prompts/context-processor.md
- Accepts bead via environment variables (HYDRA_BEAD_ID, etc.)

### FR-015b: Context-Processor Agent Integration (Part 2/2)
Create dedicated context-processor agent that runs before each bead execution.

**Acceptance Criteria:**
- Outputs JSON with routing decision and enriched context
- Integration point: called by overseer before worker spawn

### FR-016a: Context-Processor Hindsight Queries (Part 1/2)
Implement intelligence gathering from Hindsight.

**Acceptance Criteria:**
- Query Hindsight for patterns matching bead description
- Query Hindsight for similar past executions
- Aggregate results into structured context

### FR-016b: Context-Processor Hindsight Queries (Part 2/2)
Implement intelligence gathering from Hindsight.

**Acceptance Criteria:**
- Handle query failures gracefully (log warning, continue without enrichment)
- Query latency < 500ms with caching

### FR-017a: Context-Processor Decision Engine (Part 1/2)
Implement routing decision logic based on gathered intelligence.

**Acceptance Criteria:**
- Direct execution: 1-2 files, clear scope, no dependencies
- Specialist routing: 3-5 files, specific domain (testing, security, etc.)
- Consensus trigger: >5 files, architectural changes, 3+ dependencies

### FR-017b: Context-Processor Decision Engine (Part 2/2)
Implement routing decision logic based on gathered intelligence.

**Acceptance Criteria:**
- Include decision rationale in output
- Confidence score for each decision (0.0-1.0)

### FR-018a: Context-Processor Prompt Enrichment (Part 1/2)
Enrich execution prompts with gathered intelligence from Hindsight.

**Acceptance Criteria:**
- Inject relevant patterns from Hindsight query results
- Include anti-patterns to avoid
- Add execution context from similar past beads

### FR-018b: Context-Processor Prompt Enrichment (Part 2/2)
Enrich execution prompts with gathered intelligence from Hindsight.

**Acceptance Criteria:**
- Log enriched prompt to file for debugging
- Enrichment overhead < 10 seconds per bead

### FR-019a: Context-Processor Result Storage (Part 1/2)
Store context-processor results in Hindsight for learning.

**Acceptance Criteria:**
- Store under type "context-analysis"
- Include: bead_id, strategy, patterns_found, decision_rationale, confidence
- Timestamp all entries

### FR-019b: Context-Processor Result Storage (Part 2/2)
Store context-processor results in Hindsight for learning.

**Acceptance Criteria:**
- Reference original bead for traceability
- Retention: forever (part of institutional knowledge)

### FR-020a: Multi-Agent Consensus Facilitation (Part 1/2)
Implement consensus facilitator agent for complex beads.

**Acceptance Criteria:**
- Spawn when context-processor decides consensus needed
- Select 2-4 expert reviewers based on bead characteristics
- Coordinate research phase (parallel Hindsight queries)

### FR-020b: Multi-Agent Consensus Facilitation (Part 2/2)
Implement consensus facilitator agent for complex beads.

**Acceptance Criteria:**
- Manage discussion rounds (3 rounds max)
- Collect and tally votes

### FR-021a: Multi-Agent Consensus Reviewer Selection (Part 1/2)
Implement dynamic reviewer selection based on bead analysis.

**Acceptance Criteria:**
- Testing beads → test-specialist + code-reviewer
- Security beads → security-auditor + code-reviewer
- Performance beads → performance-analyst + test-specialist

### FR-021b: Multi-Agent Consensus Reviewer Selection (Part 2/2)
Implement dynamic reviewer selection based on bead analysis.

**Acceptance Criteria:**
- Architecture beads → orchestrator + code-reviewer + test-specialist
- Select from available healthy agents only

### FR-022a: Multi-Agent Consensus Voting (Part 1/2)
Implement voting mechanism for consensus decisions.

**Acceptance Criteria:**
- Reviewers vote: approve, reject, or needs_work
- Approve if 2/3 majority and no blocking concerns
- Reject if majority rejects

### FR-022b: Multi-Agent Consensus Voting (Part 2/2)
Implement voting mechanism for consensus decisions.

**Acceptance Criteria:**
- Needs_work if split or blocking concerns raised
- Store vote details: reviewer_id, vote, rationale, confidence

### FR-023a: Multi-Agent Consensus Result Storage (Part 1/2)
Store consensus results in Hindsight for learning.

**Acceptance Criteria:**
- Store under type "consensus-decisions"
- Include: bead_id, proposal, votes, outcome, dissenting_views
- Timestamp and duration

### FR-023b: Multi-Agent Consensus Result Storage (Part 2/2)
Store consensus results in Hindsight for learning.

**Acceptance Criteria:**
- Reference all participating agents
- Link to related context-analysis memory

---

## 4. Non-Functional Requirements

### NFR-001: Performance Requirements
Define performance expectations for Hindsight operations.

**Acceptance Criteria:**
- Recall latency: p95 < 500ms (with cached embeddings)
- Retain latency: < 5s (including LLM processing)
- Reflect latency: < 10s (depending on insight count)
- Memory query throughput: ≥10 ops/sec
- Server startup time: < 30s

### NFR-002: Reliability and Availability
Ensure Hindsight is a reliable mandatory dependency.

**Acceptance Criteria:**
- 99.9% uptime for Hindsight server during Hydra operation
- Automatic retry with exponential backoff for transient failures
- Health checks every 30s with 3-strike failure detection
- Circuit breaker for external LLM API calls
- Graceful degradation not permitted (fail-fast enforced)

### NFR-003: Scalability Requirements
Support target scale of 200-500 beads/day across 10 developers.

**Acceptance Criteria:**
- Support 100,000+ memories without performance degradation
- Handle 50+ concurrent memory operations
- Vector search latency remains <100ms with 100K vectors
- Database size grows linearly with memory count
- Horizontal scaling possible with external PostgreSQL

### NFR-004: Data Retention and Storage
Define storage and retention policies for memory data.

**Acceptance Criteria:**
- Retention policy: forever (no automatic deletion)
- Storage format: SQLite for local, PostgreSQL for production
- Compression enabled for large memories (>10KB)
- Automatic backup recommendations documented
- Archive strategy for memories older than 2 years

### NFR-005: Security Requirements
Ensure secure handling of memory data and API keys.

**Acceptance Criteria:**
- LLM API keys stored in environment variables only
- No secrets written to memory content or metadata
- SQLite database permissions: 0600 (owner read/write only)
- API key not logged in any output
- Optional: API key authentication for Hindsight HTTP API

### NFR-006: Error Handling and Recovery
Define error handling strategies for Hindsight operations.

**Acceptance Criteria:**
- All Hindsight errors logged with context
- Retain failures fail the bead (trigger retry)
- Recall failures logged but don't fail bead (optional enhancement)
- Server crash triggers Hydra shutdown (fail-fast)
- Automatic recovery attempts limited to 3 retries

### NFR-007: Configuration Management
Ensure flexible and validated configuration.

**Acceptance Criteria:**
- Configuration validation at Hydra startup
- Clear error messages for invalid configuration
- Environment variable override support
- Sensible defaults for all optional settings
- Configuration hot-reload not required (restart acceptable)

---

## 5. Technical Considerations

### 5.1 Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Hydra Orchestrator                        │
│  ┌──────────────┐      ┌──────────────────┐                     │
│  │   Overseer   │◄────►│  Hindsight       │                     │
│  │   (Go)       │      │  (Python)        │                     │
│  └──────────────┘      └────────┬─────────┘                     │
│       │                         │                               │
│       │    HTTP API (localhost) │                               │
│       │                         │                               │
│  ┌────┴───────┐        ┌────────┴────────┐                      │
│  │   Worker   │        │   SQLite DB     │                      │
│  │  (Go)      │        │  ~/.hydra/      │                      │
│  └────────────┘        │  hindsight.db   │                      │
│                        └─────────────────┘                      │
└─────────────────────────────────────────────────────────────────┘
```

### 5.2 Integration Points

**Point 1: Pre-Execution Context** (`overseer.go:spawnWorkerInternal()`)
- Query: Bead title + type + files
- Limit: 5 memories
- Action: Inject into worker environment

**Point 2: Post-Execution Retention** (`worker/main.go:recordExecutionResult()`)
- Data: Full execution context
- Action: Store as structured memory
- Error: Fail bead if retention fails

**Point 3: Pattern Quality** (`pattern_review.go:calculateQualityScore()`)
- Query: Similar patterns by title/type
- Action: Adjust score by historical success

**Point 4: Settled-State Reflection** (`settled_state_service.go:settleBead()`)
- Query: Bead ID + dependencies
- Action: Generate and store insights

### 5.3 Technology Stack

| Component | Technology | Version |
|-----------|-----------|---------|
| Hindsight Server | Python | 3.10+ |
| Hindsight Package | hindsight-all | Latest |
| Go Client | HTTP REST | N/A |
| Storage (Local) | SQLite | 3.35+ |
| Storage (Prod) | PostgreSQL | 15+ |
| Vector Extension | pgvector | 0.5+ |
| LLM Provider | OpenAI-compatible | Any |

### 5.4 Configuration Schema

```yaml
intelligence:
  hindsight:
    enabled: true
    api_port: 8888
    bank_id: hydra
    storage_path: ~/.hydra/hindsight.db
    llm_provider: openai
    llm_model: kimi-k2-5
    llm_api_key_env: KIMI_API_KEY  # Reuses existing env var
    llm_base_url: https://api.moonshot.cn/v1  # Kimi API endpoint
    max_results: 5
    similarity_threshold: 0.75
```

### 5.5 API Key Extraction Pattern

Reuses existing Hydra pattern for API key retrieval:

```go
// internal/core/apikey.go

// GetAPIKey retrieves an API key from environment variable
func GetAPIKey(envVarName string) (string, error) {
    if envVarName == "" {
        return "", fmt.Errorf("API key environment variable name not configured")
    }
    
    apiKey := os.Getenv(envVarName)
    if apiKey == "" {
        return "", fmt.Errorf("API key %s not found in environment", envVarName)
    }
    
    return apiKey, nil
}

// LogAPIKeyStatus logs API key loading status (without exposing the key)
func LogAPIKeyStatus(envVarName string) {
    if envVarName == "" {
        log.Printf("[Setup] Warning: API key environment variable not configured")
        return
    }
    
    apiKey := os.Getenv(envVarName)
    if apiKey == "" {
        log.Printf("[Setup] Warning: API key %s not found in environment", envVarName)
    } else {
        log.Printf("[Setup] API key %s loaded (%d chars)", envVarName, len(apiKey))
    }
}
```

**Usage:**
```go
// In cmd/hydra/main.go
LogAPIKeyStatus(config.Intelligence.Hindsight.LLMAPIKeyEnv)

// In overseer.go:spawnWorkerInternal()
if apiKey, err := GetAPIKey(o.config.Intelligence.Hindsight.LLMAPIKeyEnv); err == nil {
    env = append(env, fmt.Sprintf("%s=%s", 
        o.config.Intelligence.Hindsight.LLMAPIKeyEnv, apiKey))
}
```

### 5.6 Data Models

**Execution Memory:**
```go
type ExecutionMemory struct {
    BeadID       string
    Title        string
    Description  string
    BeadType     string
    Success      bool
    Output       string
    Error        string
    Files        []string
    Duration     time.Duration
    AgentType    string
    Timestamp    time.Time
    Project      string
    Dependencies []string
}
```

**Metadata (stored in Hindsight):**
```go
map[string]interface{}{
    "bead_id":        bead.ID.String(),
    "bead_type":      bead.Type,
    "project":        bead.Project,
    "success":        result.Success,
    "duration_sec":   result.Duration.Seconds(),
    "agent_type":     result.AgentType,
    "timestamp":      time.Now().UTC().Format(time.RFC3339),
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
    Timestamp         time.Time              `json:"timestamp"`
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

**Consensus Result:**
```go
type ConsensusResult struct {
    BeadID          string          `json:"bead_id"`
    Proposal        string          `json:"proposal"`
    Votes           []ConsensusVote `json:"votes"`
    Outcome         string          `json:"outcome"` // approved|rejected|needs_work
    DissentingViews []string        `json:"dissenting_views"`
    Participants    []string        `json:"participants"`
    Duration        time.Duration   `json:"duration"`
    Timestamp       time.Time       `json:"timestamp"`
}
```

---

## 6. Risks and Mitigation

### Risk 1: LLM API Costs
**Impact:** High  
**Probability:** Medium  
**Description:** Hindsight uses LLM for embeddings and processing, which may incur significant costs at scale.

**Mitigation:**
- Use cost-effective models (gpt-4o-mini, Kimi K2.5)
- Implement caching for embeddings
- Configure per-operation LLM selection
- Monitor and alert on usage spikes
- Support local LLMs (Ollama) for cost-sensitive environments

### Risk 2: Performance Degradation
**Impact:** High  
**Probability:** Low  
**Description:** Vector search or LLM latency could slow down bead processing.

**Mitigation:**
- Set aggressive timeouts (500ms for recall)
- Implement caching layer for frequent queries
- Use pgvectorscale (DiskANN) for large datasets
- Monitor p95/p99 latencies
- Circuit breaker for slow LLM responses

### Risk 3: Data Loss on Retention Failure
**Impact:** Medium  
**Probability:** Low  
**Description:** If Hindsight retention fails, bead results may be lost.

**Mitigation:**
- Fail-fast: Bead marked as failed, triggers retry
- Retry mechanism with exponential backoff
- Alert on repeated retention failures
- Regular health checks on Hindsight server

### Risk 4: Migration Complexity
**Impact:** Medium  
**Probability:** Medium  
**Description:** Replacing cass_memory throughout codebase may introduce bugs.

**Mitigation:**
- Comprehensive unit and integration tests
- Feature branch with thorough testing
- Side-by-side comparison during transition (optional)
- Rollback plan (revert to cass_memory if needed)

### Risk 5: LLM Provider Dependency
**Impact:** Medium  
**Probability:** Low  
**Description:** Dependency on external LLM provider may cause availability issues.

**Mitigation:**
- Support multiple providers (OpenAI, Anthropic, MiniMax, Ollama)
- Circuit breaker with fallback providers
- Local LLM support (Ollama) for offline operation
- Cached embeddings reduce API calls

---

## 7. MoSCoW Prioritization

### Must Have (M)
- **M1:** Embedded Hindsight server lifecycle management (FR-001a/b)
- **M2:** Hindsight Go client implementation (FR-002a/b)
- **M3:** Configuration system integration (FR-003a/b)
- **M4:** Pre-execution context retrieval (FR-004a/b)
- **M5:** Post-execution memory retention (FR-006a/b)
- **M6:** Mandatory retention error handling (FR-007a/b)
- **M7:** Context-processor agent integration (FR-015a/b)
- **M8:** Context-processor Hindsight queries (FR-016a/b)
- **M9:** cass_memory deprecation and removal (FR-014a/b)
- **M10:** Performance requirements (NFR-001)
- **M11:** Reliability and availability (NFR-002)

### Should Have (S)
- **S1:** Worker context parsing (FR-005a/b)
- **S2:** Pattern quality scoring enhancement (FR-008a/b)
- **S3:** Settled-state reflection (FR-009a/b)
- **S4:** Context-processor decision engine (FR-017a/b)
- **S5:** Context-processor prompt enrichment (FR-018a/b)
- **S6:** Context-processor result storage (FR-019a/b)
- **S7:** Observability and logging (FR-013a/b)
- **S8:** Security requirements (NFR-005)
- **S9:** Error handling and recovery (NFR-006)

### Could Have (C)
- **C1:** Cross-project memory bank (FR-010a/b)
- **C2:** LLM provider abstraction (FR-011a/b)
- **C3:** Multi-agent consensus facilitation (FR-020a/b)
- **C4:** Multi-agent consensus voting (FR-022a/b)
- **C5:** Scalability requirements (NFR-003)
- **C6:** Data retention and storage (NFR-004)

### Won't Have (W)
- **W1:** Hindsight skills for Pi agents (out of scope)
- **W2:** UI/dashboard for memory management
- **W3:** Advanced analytics and reporting
- **W4:** Multi-tenant isolation
- **W5:** Configuration hot-reload
- **W6:** Multi-agent consensus reviewer selection complexity (start with hardcoded mapping)

---

## 8. Success Metrics

### 8.1 Technical Metrics

| Metric | Target | Measurement Method |
|--------|--------|-------------------|
| Recall latency (p95) | < 500ms | Prometheus histogram |
| Retain latency (p95) | < 5s | Prometheus histogram |
| Server uptime | 99.9% | Health check monitoring |
| Memory retention rate | 100% | Bead completion tracking |
| Error rate | < 1% | Error log aggregation |

### 8.2 Business Metrics

| Metric | Target | Measurement Method |
|--------|--------|-------------------|
| Cross-project pattern reuse | ≥30% | Pattern application tracking |
| Time-to-completion reduction | ≥40% | A/B testing with/without Hindsight |
| Developer productivity gain | ≥25% | Tasks completed per day |
| Pattern suggestion acceptance | ≥75% | User feedback tracking |
| Bead throughput | 200-500/day | Daily bead completion count |

### 8.3 Adoption Metrics

| Metric | Target | Measurement Method |
|--------|--------|-------------------|
| Knowledge sharing coverage | 100% | % of projects using Hindsight |
| Memory count growth | Linear | Total memories over time |
| Query success rate | ≥95% | Successful recall / total recall |
| Developer satisfaction | ≥4.2/5 | Post-implementation survey |

---

## 9. Research Insights

### 9.1 Codebase Research Findings

**From `codebase-researcher`:**
- Hydra has clean abstraction layers making service integration straightforward
- Current cass_memory integration follows skills-based architecture (not force-feeding)
- Configuration system supports hierarchical structure with hot-reload capability
- Error handling uses graceful degradation for non-critical services
- Pattern extraction occurs in settled-state service with 2-minute delay

**Key Integration Points Identified:**
1. `overseer.go:spawnWorkerInternal()` - Pre-execution context
2. `worker/main.go:recordExecutionResult()` - Post-execution recording
3. `pattern_review.go:calculateQualityScore()` - Quality enhancement
4. `settled_state_service.go:settleBead()` - Reflection

### 9.2 Historical Context

**From `git-history-analyzer`:**
- cass_memory added March 8, 2026 with initial Phase 2 implementation
- Architecture pivoted from force-feeding to skills-based March 13, 2026
- Force-feeding caused 2,300+ Node process explosions with concurrent workers
- Lesson: On-demand querying (skills) achieves 75-90% token savings vs force-feeding
- Pattern: All intelligence services follow client interface with `IsEnabled()` check

**Migration Pattern:**
- Phase 1: Implement new service alongside existing
- Phase 2: Test with small bead set (10-20)
- Phase 3: Tune configuration
- Phase 4: Scale to production (122+ beads)

### 9.3 Best Practices Applied

**From `best-practices-researcher`:**
- **Memory Architecture**: 3-tier approach (working/semantic/episodic) with async persistence
- **Vector Search**: HNSW indexing with hybrid search (dense + sparse) and RRF reranking
- **LLM Abstraction**: Unified interface with circuit breakers and cost optimization
- **Error Handling**: Classify as critical (fail-fast) vs non-critical (graceful degradation)
- **Service Lifecycle**: Dependency-based startup/shutdown with health monitoring

**Configuration Decisions:**
- Vector DB: PostgreSQL + pgvector (single operational overhead)
- Embedding Model: text-embedding-3-large (quality/cost ratio)
- Retry Strategy: Exponential backoff, max 3 attempts
- Circuit Breaker: 5 failures, 30s recovery

### 9.4 Hindsight Library Capabilities

**From `library-source-reader`:**
- Three core operations: Retain, Recall, Reflect
- Embedded mode available via `hindsight-all` Python package
- Supports 8+ LLM providers (OpenAI, Anthropic, MiniMax, Ollama, etc.)
- Uses PostgreSQL exclusively (pgvector for vectors)
- Go client auto-generated from OpenAPI spec
- Requires Python 3.10+, 2GB+ RAM, ~500MB disk

**Go Client Limitations:**
- No embedded mode (requires running server)
- Extensive use of nullable types for optional fields
- No built-in retry logic (must implement separately)

### 9.5 Domain Expertise

**From `domain-specialist`:**
- **TDD Enforcement**: Iron law requires failing test before production code
- **Pattern Quality**: Novel + Generalizable + Documented + Proven + Non-obvious = 100 points
- **Settled-State**: Multi-signal detection (git merge + DAG stability + time)
- **Cross-Project**: Patterns transcend boundaries automatically
- **Knowledge Lifecycle**: Candidate → Pending Review → Experimental → Stable → Deprecated

**Metrics from E2E Testing:**
- Pattern extraction rate: ≥90% of qualifying beads
- Time-to-completion reduction: ≥40% with relevant patterns
- Pattern suggestion acceptance: ≥75% when confidence >0.85
- Token efficiency: 75-90% savings with skills-based vs force-feeding

---

## 10. Appendix

### 10.1 Glossary

- **Bead**: A discrete task in the Hydra system (unit of work)
- **Hindsight**: Vectorize.io's agent memory system
- **Bank**: Hindsight's namespace for memories (we use "hydra")
- **Retain**: Store information in Hindsight
- **Recall**: Retrieve memories from Hindsight
- **Reflect**: Generate insights from existing memories
- **cass_memory**: Current institutional knowledge system (being replaced)
- **Settled State**: Immutable state of a bead ready for pattern extraction

### 10.2 References

- Hindsight Documentation: https://hindsight.vectorize.io
- Hindsight GitHub: https://github.com/vectorize-io/hindsight
- Hydra AGENTS.md: Current agent configuration and workflows
- Collective Unconscious PRD: Cross-project pattern sharing specification

### 10.3 Revision History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-03-16 | Hydra Team | Initial PRD creation |
| 1.1 | 2026-03-16 | Hydra Team | Validated and atomicity-compliant version |

---

**End of Document**

**Next Steps:**
1. Review PRD with stakeholders
2. Approve for implementation
3. Create feature branch: `feature/hindsight-integration`
4. Begin Phase 0 implementation (Foundation)
