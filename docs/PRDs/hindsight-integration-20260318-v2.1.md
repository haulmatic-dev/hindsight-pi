# Product Requirements Document: Hindsight Memory System Integration

**Date:** March 17, 2026  
**Version:** 2.1  
**Status:** Validated - Ready for Implementation  
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
- Manual discovery beads blocking implementation automation

**Expected Improvements:**
- **≥40% reduction** in time-to-completion for similar tasks across projects
- **≥30% cross-project pattern reuse rate** through shared memory banks
- **200-500 beads/day capacity** supporting 10 developers
- **Compounding institutional intelligence** that improves with every solved problem
- **≥75% one-shot success rate** through continuous expertise building

### 1.3 Success Metrics

| Metric | Current (cass_memory) | Target (Hindsight) | Measurement |
|--------|----------------------|-------------------|-------------|
| Pattern reuse | 0% cross-project | ≥30% cross-project | Pattern application tracking |
| Time-to-completion | Baseline | -40% | Execution duration comparison |
| Daily bead throughput | ~100/day | 200-500/day | Bead completion rate |
| Knowledge retention | Per-project | Organization-wide | Cross-project query success |
| Developer productivity | Baseline | +25% | Tasks completed per day |
| One-shot success rate | ~40-50% | ≥75% | First-attempt task completion |
| Memory retrieval accuracy | N/A | ≥85% | Precision@K of retrieved context |
| Recall latency (p95) | N/A | <500ms | Response time measurement |

### 1.4 Scope

**In Scope:**
- Complete replacement of `cass_memory` with Hindsight
- Embedded Hindsight server lifecycle management
- Four deterministic integration points (recall, retain, reflect, quality scoring)
- Cross-project memory sharing with unified "hydra" bank
- **Continuous discovery architecture**
- **Multi-dimensional memory tagging**
- **Context-processor agent with confidence scoring**
- **A/B prompt testing framework**
- **Utility-weighted retention policies**
- **Failure mode analysis**
- Fail-fast error handling (mandatory dependency)

**Out of Scope:**
- Hindsight skills for Pi agents (Phase 2 enhancement)
- UI/dashboard for memory management
- Advanced analytics and reporting
- Multi-tenant isolation (single organization)
- Separate OpenClaw-RL integration (patterns only, not full framework)

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

### US-008: Continuous Discovery Without Blocking
> As a developer, I want discovery to happen continuously in the background without blocking bead execution, so that research findings are available when needed without delays.

**Acceptance Criteria:**
- Async discovery triggered when knowledge confidence <0.40
- Discovery runs in parallel with bead execution
- Findings stored in Hindsight for future queries
- No blocking of critical path execution flow

### US-009: Multi-Dimensional Memory Tagging
> As a system architect, I want to tag memories with multiple dimensions (language, domain, pattern type), so that retrieval is more precise and contextually relevant.

**Acceptance Criteria:**
- Support language tags: go, typescript, python, etc.
- Support domain tags: testing, security, architecture, debugging
- Support pattern type tags: anti-pattern, best-practice, workaround
- Support abstraction level tags: code-snippet, algorithm, architectural
- Query can filter by any combination of tags

### US-010: A/B Prompt Testing
> As a quality engineer, I want to A/B test different prompt variations, so that we continuously improve one-shot success rates.

**Acceptance Criteria:**
- System can test 2-3 prompt variations per bead type
- Success metrics tracked per variant
- Statistical significance testing (p < 0.05)
- Winning prompts auto-promoted after validation

### US-011: Failure Mode Analysis
> As a developer, I want the system to learn from failures and suggest fixes based on similar past failures, so that debugging time is reduced.

**Acceptance Criteria:**
- When bead fails, query Hindsight for "similar failures"
- Extract common root causes from failure patterns
- Suggest fixes based on what worked before
- Track Mean Time To Resolution (MTTR) improvements

### US-012: Expertise Dashboard
> As a product manager, I want visibility into Hydra's learning progress, so that we can measure ROI and identify knowledge gaps.

**Acceptance Criteria:**
- Dashboard showing one-shot success rate over time
- Most valuable patterns (high recall + high success)
- Knowledge gaps (frequently queried, low results)
- Cross-project pattern reuse metrics

---

## 3. Functional Requirements

### FR-001a: Embedded Hindsight Server Lifecycle Management (Part 1/2)
Initialize and manage the Hindsight embedded Python server as a mandatory dependency of Hydra.

**Acceptance Criteria:**
- Server starts automatically when Hydra initializes
- Health check validates server is ready before accepting beads
- Server stops gracefully on Hydra shutdown

### FR-001b: Embedded Hindsight Server Lifecycle Management (Part 2/2)
Server configuration and failure handling for Hindsight embedded Python server.

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
Client health monitoring and error handling for Hindsight Go HTTP client.

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
Configuration validation and defaults for Hindsight system integration.

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
Context serialization and logging for pre-execution retrieval.

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
Error handling for missing or malformed context in worker process.

**Acceptance Criteria:**
- Graceful handling if context is missing or malformed

### FR-006a: Post-Execution Memory Retention (Part 1/2)
Replace `cass_memory.AddPattern()` with Hindsight `Retain()` in worker completion flow.

**Acceptance Criteria:**
- `recordExecutionResult()` calls `hindsight.Retain()` instead of `cass_memory`
- Memory includes structured data: bead ID, title, type, success, output, error, files, duration
- Metadata includes timestamp, project, agent type

### FR-006b: Post-Execution Memory Retention (Part 2/2)
Failure handling and retry logic for post-execution memory retention.

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
Error visibility and data loss prevention for retention failures.

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
Quality scoring thresholds and constraints for pattern evaluation.

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
Insight linking and non-blocking execution for settled-state reflection.

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
Project filtering and knowledge sharing configuration for memory bank.

**Acceptance Criteria:**
- Ability to filter queries by project when needed
- No project isolation (intentional for knowledge sharing)

### FR-011a: Context-Processor Agent Integration (Part 1/2)
Create dedicated context-processor agent that runs before each bead execution.

**Acceptance Criteria:**
- New agent type: context-processor
- Agent defined in .agents/prompts/context-processor.md
- Accepts bead via environment variables (HYDRA_BEAD_ID, etc.)

### FR-011b: Context-Processor Agent Integration (Part 2/2)
Agent output format and overseer integration for context-processor.

**Acceptance Criteria:**
- Outputs JSON with routing decision and enriched context
- Integration point: called by overseer before worker spawn

### FR-012a: Context-Processor Hindsight Queries (Part 1/2)
Implement intelligence gathering from Hindsight.

**Acceptance Criteria:**
- Query Hindsight for patterns matching bead description
- Query Hindsight for similar past executions
- Aggregate results into structured context

### FR-012b: Context-Processor Hindsight Queries (Part 2/2)
Query failure handling and performance optimization for Hindsight queries.

**Acceptance Criteria:**
- Handle query failures gracefully (log warning, continue without enrichment)
- Query latency < 500ms with caching

### FR-013a: Context-Processor Decision Engine (Part 1/2)
Implement routing decision logic based on gathered intelligence.

**Acceptance Criteria:**
- Direct execution: 1-2 files, clear scope, no dependencies
- Specialist routing: 3-5 files, specific domain (testing, security, etc.)
- Consensus trigger: >5 files, architectural changes, 3+ dependencies

### FR-013b: Context-Processor Decision Engine (Part 2/2)
Decision output format and confidence scoring for routing decisions.

**Acceptance Criteria:**
- Include decision rationale in output
- Confidence score for each decision (0.0-1.0)

### FR-014a: Context-Processor Prompt Enrichment (Part 1/2)
Enrich execution prompts with gathered intelligence from Hindsight.

**Acceptance Criteria:**
- Inject relevant patterns from Hindsight query results
- Include anti-patterns to avoid
- Add execution context from similar past beads

### FR-014b: Context-Processor Prompt Enrichment (Part 2/2)
Enrichment logging and performance constraints for prompt enhancement.

**Acceptance Criteria:**
- Log enriched prompt to file for debugging
- Enrichment overhead < 10 seconds per bead

### FR-015a: Context-Processor Result Storage (Part 1/2)
Store context-processor results in Hindsight for learning.

**Acceptance Criteria:**
- Store under type "context-analysis"
- Include: bead_id, strategy, patterns_found, decision_rationale, confidence
- Timestamp all entries

### FR-015b: Context-Processor Result Storage (Part 2/2)
Traceability and retention policy for context-processor results.

**Acceptance Criteria:**
- Reference original bead for traceability
- Retention: forever (part of institutional knowledge)

### FR-016a: Continuous Discovery Service (Part 1/2)
Implement async discovery that triggers when knowledge confidence is low.

**Acceptance Criteria:**
- Discovery triggered when confidence score <0.40
- Service runs discovery in background (non-blocking)
- Queries external sources, documentation, codebase

### FR-016b: Continuous Discovery Service (Part 2/2)
Discovery result storage and knowledge update for continuous discovery.

**Acceptance Criteria:**
- Stores findings in Hindsight with "discovery" type
- Updates context-processor with new knowledge

### FR-017a: Multi-Dimensional Tagging System (Part 1/2)
Implement comprehensive tagging for memories.

**Acceptance Criteria:**
- Support language dimension: go, typescript, python, etc.
- Support domain dimension: testing, security, architecture, debugging
- Support pattern_type dimension: anti-pattern, best-practice, workaround

### FR-017b: Multi-Dimensional Tagging System (Part 2/2)
Abstraction level tags and query API for multi-dimensional tagging.

**Acceptance Criteria:**
- Support abstraction_level dimension: code-snippet, algorithm, architectural
- Tags stored in memory metadata
- Query API supports tag filtering with AND/OR logic

### FR-018a: A/B Prompt Testing Framework (Part 1/2)
Implement A/B testing for prompt optimization.

**Acceptance Criteria:**
- Support 2-3 prompt variants per bead type
- Random assignment of beads to variants
- Track success rate per variant

### FR-018b: A/B Prompt Testing Framework (Part 2/2)
Statistical analysis and auto-promotion for A/B prompt testing.

**Acceptance Criteria:**
- Statistical significance testing (p < 0.05)
- Auto-promote winning variant after 30+ samples

### FR-019a: Multi-Agent Consensus Facilitation (Part 1/2)
Implement consensus facilitator agent for complex beads.

**Acceptance Criteria:**
- Spawn when context-processor decides consensus needed
- Select 2-4 expert reviewers based on bead characteristics
- Coordinate research phase (parallel Hindsight queries)

### FR-019b: Multi-Agent Consensus Facilitation (Part 2/2)
Discussion management and vote collection for consensus facilitation.

**Acceptance Criteria:**
- Manage discussion rounds (3 rounds max)
- Collect and tally votes

### FR-020a: Multi-Agent Consensus Reviewer Selection (Part 1/2)
Implement dynamic reviewer selection based on bead analysis.

**Acceptance Criteria:**
- Testing beads → test-specialist + code-reviewer
- Security beads → security-auditor + code-reviewer
- Performance beads → performance-analyst + test-specialist

### FR-020b: Multi-Agent Consensus Reviewer Selection (Part 2/2)
Architecture beads and health checks for reviewer selection.

**Acceptance Criteria:**
- Architecture beads → orchestrator + code-reviewer + test-specialist
- Select from available healthy agents only

### FR-021a: Multi-Agent Consensus Voting (Part 1/2)
Implement voting mechanism for consensus decisions.

**Acceptance Criteria:**
- Reviewers vote: approve, reject, or needs_work
- Approve if 2/3 majority and no blocking concerns
- Reject if majority rejects

### FR-021b: Multi-Agent Consensus Voting (Part 2/2)
Split decisions and vote detail storage for consensus voting.

**Acceptance Criteria:**
- Needs_work if split or blocking concerns raised
- Store vote details: reviewer_id, vote, rationale, confidence

### FR-022a: Multi-Agent Consensus Result Storage (Part 1/2)
Store consensus results in Hindsight for learning.

**Acceptance Criteria:**
- Store under type "consensus-decisions"
- Include: bead_id, proposal, votes, outcome, dissenting_views
- Timestamp and duration

### FR-022b: Multi-Agent Consensus Result Storage (Part 2/2)
Agent references and memory linking for consensus results.

**Acceptance Criteria:**
- Reference all participating agents
- Link to related context-analysis memory

### FR-023a: Failure Mode Analysis Service (Part 1/2)
Implement automatic failure analysis and fix suggestion.

**Acceptance Criteria:**
- On bead failure, query Hindsight for similar failures
- Extract common root causes
- Suggest fixes based on historical success patterns

### FR-023b: Failure Mode Analysis Service (Part 2/2)
MTTR tracking and analysis storage for failure mode analysis.

**Acceptance Criteria:**
- Track MTTR improvements over time
- Store failure analysis in Hindsight

### FR-024a: Utility-Weighted Retention Policy (Part 1/3)
Implement insight-based memory retention (not time-based).

**Acceptance Criteria:**
- Calculate utility score: U = α·frequency + β·recency + γ·relevance
- Define insight depth levels (1-5) with different retention periods
- Level 5 (Mental Models): Retain forever

### FR-024b: Utility-Weighted Retention Policy (Part 2/3)
Intermediate retention periods for utility-weighted retention.

**Acceptance Criteria:**
- Level 4 (Architectural): Retain 3 years
- Level 3 (Best Practices): Retain 2 years
- Level 2 (Specific Solutions): Retain 1 year

### FR-024c: Utility-Weighted Retention Policy (Part 3/3)
Routine retention and garbage collection for utility-weighted policy.

**Acceptance Criteria:**
- Level 1 (Routine): Retain 6-8 months
- Async garbage collection during low usage

### FR-025a: Expertise Dashboard (Part 1/2)
Implement dashboard for tracking learning progress.

**Acceptance Criteria:**
- Display one-shot success rate over time
- Show most valuable patterns (high recall + high success)
- Display knowledge gaps (frequently queried, low results)

### FR-025b: Expertise Dashboard (Part 2/2)
Cross-project metrics and export capabilities for expertise dashboard.

**Acceptance Criteria:**
- Show cross-project pattern reuse metrics
- Export capabilities for reporting

### FR-026a: LLM Provider Abstraction (Part 1/2)
Support multiple LLM providers through configuration.

**Acceptance Criteria:**
- Support OpenAI-compatible APIs (base_url configuration)
- Support native MiniMax integration
- API key retrieved via modular extraction function

### FR-026b: LLM Provider Abstraction (Part 2/2)
Environment variable configuration and secure logging for LLM providers.

**Acceptance Criteria:**
- Environment variable name configurable via `llm_api_key_env` setting
- API key logged with length only (never expose key value)

### FR-027a: SQLite Storage Backend (Part 1/2)
Use SQLite for embedded Hindsight storage in local development.

**Acceptance Criteria:**
- SQLite database stored at configurable path (default: ~/.hydra/hindsight.db)
- Automatic schema migration on startup
- pgvector extension not required for SQLite

### FR-027b: SQLite Storage Backend (Part 2/2)
Security and backup utilities for SQLite storage.

**Acceptance Criteria:**
- Database file permissions set to 0600
- Backup/restore utilities documented

### FR-028a: Observability and Logging (Part 1/2)
Implement comprehensive logging for Hindsight operations.

**Acceptance Criteria:**
- Server lifecycle events logged (start, stop, health check)
- All memory operations logged with duration and result count
- Error logs include detailed context and stack traces

### FR-028b: Observability and Logging (Part 2/2)
Log format standardization and security for Hindsight operations.

**Acceptance Criteria:**
- Log format: `[HINDSIGHT] <operation> <bead_id> <result>`
- No sensitive data (API keys, secrets) logged

### FR-029a: cass_memory Deprecation and Removal (Part 1/2)
Remove all cass_memory dependencies and code.

**Acceptance Criteria:**
- `internal/intelligence/cass_memory.go` deleted
- All cass_memory imports removed from codebase
- cass_memory client initialization removed from overseer

### FR-029b: cass_memory Deprecation and Removal (Part 2/2)
Code replacement and documentation update for cass_memory removal.

**Acceptance Criteria:**
- cass_memory calls replaced with Hindsight equivalents
- Documentation updated to remove cass_memory references

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
- Retention policy: utility-weighted (not time-based)
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

### NFR-008: Memory Quality Standards
Define quality requirements for stored memories.

**Acceptance Criteria:**
- Minimum retrieval accuracy: ≥85%
- Context relevance score: ≥0.7
- Memory compression ratio: 10:1
- Pattern success rate: ≥90% for accepted patterns
- False positive rate for suggestions: <10%

---

## 5. Technical Considerations

### 5.1 Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Hydra Orchestrator                        │
│  ┌──────────────┐      ┌──────────────────┐                     │
│  │   Overseer   │◄────►│  Hindsight       │                     │
│  │   (Go)       │      │  (Python)        │                     │
│  └──────┬───────┘      └────────┬─────────┘                     │
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
- **Requirements:** US-001, FR-004a, FR-004b, FR-011, FR-012, FR-013, FR-014
- Query: Bead title + type + files
- Limit: 5 memories
- Action: Inject into worker environment

**Point 2: Post-Execution Retention** (`worker/main.go:recordExecutionResult()`)
- **Requirements:** US-002, FR-006a, FR-006b, FR-007
- Data: Full execution context
- Action: Store as structured memory
- Error: Fail bead if retention fails

**Point 3: Pattern Quality** (`pattern_review.go:calculateQualityScore()`)
- **Requirements:** US-003, FR-008
- Query: Similar patterns by title/type
- Action: Adjust score by historical success

**Point 4: Settled-State Reflection** (`settled_state_service.go:settleBead()`)
- **Requirements:** US-004, FR-009, FR-010
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
    recall_budget: mid  # low/mid/high
    reflect_budget: low  # for performance
```

### 5.5 Multi-Dimensional Tagging Schema

```go
// Memory metadata structure
map[string]interface{}{
    "bead_id":        bead.ID.String(),
    "bead_type":      bead.Type,
    "project":        bead.Project,
    "success":        result.Success,
    "duration_sec":   result.Duration.Seconds(),
    "agent_type":     result.AgentType,
    "timestamp":      time.Now().UTC().Format(time.RFC3339),
    // Multi-dimensional tags
    "language":       []string{"go", "typescript"},  // Can have multiple
    "domain":         []string{"testing", "security"},
    "pattern_type":   "best-practice",
    "abstraction_level": "architectural",
    "insight_depth":  4,  // 1-5 scale
}
```

### 5.6 Data Models

**ExecutionMemory** (Requirements: US-002, FR-006a, FR-006b, FR-007a, FR-007b)
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

**ContextResult** (Requirements: US-006, FR-011a, FR-011b, FR-012a, FR-012b, FR-013a, FR-013b, FR-014a, FR-014b, FR-015a, FR-015b)
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

**ConsensusVote** (Requirements: US-007, FR-019a, FR-019b, FR-020a, FR-020b, FR-021a, FR-021b, FR-022a, FR-022b)
```go
type ConsensusVote struct {
    ReviewerID string  `json:"reviewer_id"`
    Vote       string  `json:"vote"` // approve|reject|needs_work
    Rationale  string  `json:"rationale"`
    Confidence float64 `json:"confidence"`
}
```

**ConsensusResult** (Requirements: US-007, FR-019a, FR-019b, FR-022a, FR-022b)
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

### 5.7 Utility-Weighted Retention Formula

```go
// Calculate utility score for memory retention
func CalculateUtilityScore(memory Memory) float64 {
    recencyWeight := 0.3
    successWeight := 0.4
    usageWeight := 0.2
    richnessWeight := 0.1
    
    daysSinceCreation := time.Since(memory.Timestamp).Hours() / 24
    recencyScore := 1.0 / (1.0 + daysSinceCreation/30.0)  // Decay over months
    
    utilityScore := 
        recencyWeight*recencyScore +
        successWeight*memory.SuccessRate +
        usageWeight*float64(memory.RecallCount)*0.01 +
        richnessWeight*float64(memory.InsightDepth)*0.2
    
    return utilityScore
}

// Retention periods by insight depth
var RetentionPeriods = map[int]time.Duration{
    5: time.Duration(math.MaxInt64),  // Forever (mental models)
    4: 3 * 365 * 24 * time.Hour,      // 3 years (architectural)
    3: 2 * 365 * 24 * time.Hour,      // 2 years (best practices)
    2: 1 * 365 * 24 * time.Hour,      // 1 year (specific solutions)
    1: 180 * 24 * time.Hour,          // 6 months (routine)
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
- Use batch API for 50% cost reduction

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

### Risk 6: Memory Bloat
**Impact:** Medium  
**Probability:** Medium  
**Description:** Without proper retention policies, memory storage could grow unbounded.

**Mitigation:**
- Implement utility-weighted retention with automatic cleanup
- Regular garbage collection during low-usage periods
- Archive strategy for old memories
- Monitor storage growth and alert on thresholds

### Risk 7: Context-Processor Bottleneck
**Impact:** High  
**Probability:** Low  
**Description:** Context-processor could become bottleneck if not performant.

**Mitigation:**
- Query latency <500ms requirement
- Caching layer for frequent queries
- Async processing where possible
- Horizontal scaling capability
- Circuit breaker for slow queries

---

## 7. MoSCoW Prioritization

### 7.1 Phase 1: Foundation (Must Have - Weeks 1-2)

**M1:** Embedded Hindsight server lifecycle management (FR-001a, FR-001b)  
**M2:** Hindsight Go client implementation (FR-002a, FR-002b)  
**M3:** Configuration system integration (FR-003a, FR-003b)  
**M4:** Pre-execution context retrieval (FR-004a, FR-004b)  
**M5:** Post-execution memory retention (FR-006a, FR-006b)  
**M6:** Mandatory retention error handling (FR-007a, FR-007b)  
**M7:** Pattern quality scoring enhancement (FR-008a, FR-008b)  
**M8:** Settled-state reflection (FR-009a, FR-009b)  
**M9:** Cross-project memory bank (FR-010a, FR-010b)  
**M10:** Performance requirements (NFR-001)  
**M11:** Reliability and availability (NFR-002)  
**M12:** Worker context parsing (FR-005a, FR-005b)

### 7.2 Phase 2: Intelligence (Must Have - Weeks 3-4)

**M13:** Context-processor agent integration (FR-011a, FR-011b)  
**M14:** Context-processor Hindsight queries (FR-012a, FR-012b)  
**M15:** Context-processor decision engine (FR-013a, FR-013b)  
**M16:** Context-processor prompt enrichment (FR-014a, FR-014b)  
**M17:** Context-processor result storage (FR-015a, FR-015b)  
**M18:** Continuous discovery service (FR-016a, FR-016b)  
**M19:** Multi-dimensional tagging system (FR-017a, FR-017b)  
**M20:** Security requirements (NFR-005)  
**M21:** Error handling and recovery (NFR-006)

### 7.3 Phase 3: Optimization (Should Have - Weeks 5-6)

**S1:** A/B prompt testing framework (FR-018a, FR-018b)  
**S2:** Multi-agent consensus facilitation (FR-019a, FR-019b)  
**S3:** Multi-agent consensus reviewer selection (FR-020a, FR-020b)  
**S4:** Multi-agent consensus voting (FR-021a, FR-021b)  
**S5:** Multi-agent consensus result storage (FR-022a, FR-022b)  
**S6:** Failure mode analysis service (FR-023a, FR-023b)  
**S7:** Memory quality standards (NFR-008)

### 7.4 Phase 4: Scale (Could Have - Weeks 7-8)

**C1:** Utility-weighted retention policy (FR-024a, FR-024b, FR-024c)  
**C2:** Expertise dashboard (FR-025a, FR-025b)  
**C3:** LLM provider abstraction (FR-026a, FR-026b)  
**C4:** SQLite storage backend (FR-027a, FR-027b)  
**C5:** Observability and logging (FR-028a, FR-028b)  
**C6:** cass_memory deprecation and removal (FR-029a, FR-029b)  
**C7:** Scalability requirements (NFR-003)  
**C8:** Data retention and storage (NFR-004)  
**C9:** Configuration management (NFR-007)

### 7.5 Won't Have

**W1:** Hindsight skills for Pi agents (out of scope)  
**W2:** UI/dashboard for memory management (Phase 3)  
**W3:** Advanced analytics and reporting (Phase 3)  
**W4:** Multi-tenant isolation (single organization assumption)  
**W5:** Configuration hot-reload (restart acceptable)  
**W6:** Separate OpenClaw-RL framework (patterns only, not full RL)

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
| Memory retrieval accuracy | ≥85% | Precision@K measurement |
| Context relevance score | ≥0.7 | Cosine similarity analysis |

### 8.2 Business Metrics

| Metric | Target | Measurement Method |
|--------|--------|-------------------|
| Cross-project pattern reuse | ≥30% | Pattern application tracking |
| Time-to-completion reduction | ≥40% | A/B testing with/without Hindsight |
| Developer productivity gain | ≥25% | Tasks completed per day |
| Pattern suggestion acceptance | ≥75% | User feedback tracking |
| Bead throughput | 200-500/day | Daily bead completion count |
| One-shot success rate | ≥75% | First-attempt completion tracking |
| Mean Time To Resolution | <15 min | Debugging time measurement |

### 8.3 Quality Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Pattern extraction rate | ≥90% | % of qualifying beads with patterns |
| Quality score average | ≥0.80 | Average pattern quality score |
| False positive rate | <10% | % of irrelevant pattern suggestions |
| Security scan pass rate | 100% | % of patterns passing security checks |
| Test coverage (generated code) | >80% | Lines covered by tests |
| Cyclomatic complexity | <15 | Per function static analysis |

### 8.4 Adoption Metrics

| Metric | Target | Measurement Method |
|--------|--------|-------------------|
| Knowledge sharing coverage | 100% | % of projects using Hindsight |
| Memory count growth | Linear | Total memories over time |
| Query success rate | ≥95% | Successful recall / total recall |
| Developer satisfaction | ≥4.2/5 | Post-implementation survey |
| A/B test participation | 100% | % of bead types with active tests |

---

## 9. Research Insights

### 9.1 Codebase Research Findings

**From `codebase-researcher`:**
- Hydra has clean abstraction layers making service integration straightforward
- Current cass_memory integration follows skills-based architecture (not force-feeding)
- Configuration system supports hierarchical structure with hot-reload capability
- Error handling uses graceful degradation for non-critical services
- Pattern extraction occurs in settled-state service with 2-minute delay
- Key integration points identified at 4 locations in codebase
- Worker architecture supports environment variable context injection

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
- TLDR indexer implemented with queue-based processing (size: 100, concurrency: 2)

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
- **A/B Testing**: Version all prompts, statistical significance testing, canary deployments
- **Multi-dimensional Tagging**: 3-5 tag dimensions with ontology relationships
- **Utility-Weighted Retention**: U = α·frequency + β·recency + γ·relevance formula

**Configuration Decisions:**
- Vector DB: PostgreSQL + pgvector (single operational overhead)
- Embedding Model: text-embedding-3-large (quality/cost ratio)
- Retry Strategy: Exponential backoff, max 3 attempts
- Circuit Breaker: 5 failures, 30s recovery

### 9.4 Hindsight Library Capabilities

**From `library-source-reader`:**
- Three core operations: Retain, Recall, Reflect
- TEMPR retrieval: 4 parallel strategies (Semantic, Keyword, Graph, Temporal)
- Embedded mode available via `hindsight-all` Python package
- Supports 8+ LLM providers (OpenAI, Anthropic, MiniMax, Ollama, etc.)
- Uses PostgreSQL exclusively (pgvector for vectors)
- Go client auto-generated from OpenAPI spec
- Requires Python 3.10+, 2GB+ RAM, ~500MB disk
- Performance: Recall <600ms, Retain <2s, supports 100K+ memories

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
- **One-Shot Success**: Target ≥75% with progressive context building and pre-validation
- **Consensus**: Weighted voting based on historical accuracy, supermajority for critical decisions
- **Failure Analysis**: Structured taxonomy with MTTR <15 min target

**Metrics from E2E Testing:**
- Pattern extraction rate: ≥90% of qualifying beads
- Time-to-completion reduction: ≥40% with relevant patterns
- Pattern suggestion acceptance: ≥75% when confidence >0.85
- Token efficiency: 75-90% savings with skills-based vs force-feeding

### 9.6 OpenClaw-RL Pattern Integration

**Applied Patterns:**
- **Trajectory Tracking**: Discovery → Implementation → Testing session awareness
- **OPD (On-Policy Distillation)**: Rich hints better than binary feedback in memories
- **Async Components**: Discovery runs parallel to execution (non-blocking)
- **Utility Scoring**: Value-based retention instead of time-based expiration
- **Continuous Learning**: Feedback loops with experience replay

**Implementation Notes:**
- Not integrating full OpenClaw-RL framework (scope control)
- Adapting key patterns for Hindsight integration
- Focus on memory/trajectory concepts, not policy optimization

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
- **Context-Processor**: Pre-execution agent that analyzes and routes beads
- **Consensus**: Multi-agent voting mechanism for complex decisions
- **TEMPR**: Token-Efficient Multi-Path Retrieval (Hindsight's 4-strategy search)
- **OPD**: On-Policy Distillation (rich feedback pattern)

### 10.2 Three-Phase Implementation Timeline

| Phase | Duration | Focus | Success Criteria |
|-------|----------|-------|------------------|
| **Phase 1** | Weeks 1-2 | Foundation | Hindsight server operational, 4 integration points working |
| **Phase 2** | Weeks 3-4 | Intelligence | Context-processor routing beads with confidence scoring |
| **Phase 3** | Weeks 5-6 | Optimization | A/B testing active, consensus for complex beads |
| **Phase 4** | Weeks 7-8 | Scale | 100K+ memories, utility-weighted retention, dashboard |

### 10.3 Confidence Thresholds

```
Confidence Score Interpretation:
├─ ≥0.75: High confidence → Direct execution
├─ 0.40-0.74: Medium → Execution with caution flag
└─ <0.40: Low → Trigger async discovery + mark bead for enrichment
```

### 10.4 Tagging Examples

```yaml
# Example memory with multi-dimensional tags
memory:
  content: "Use defer for resource cleanup in Go"
  type: "best-practice"
  tags:
    language: ["go"]
    domain: ["resource-management"]
    pattern_type: "best-practice"
    abstraction_level: "code-snippet"
  metadata:
    insight_depth: 3
    success_rate: 0.94
    usage_count: 156
```

### 10.5 References

- Hindsight Documentation: https://hindsight.vectorize.io
- Hindsight GitHub: https://github.com/vectorize-io/hindsight
- Hydra AGENTS.md: Current agent configuration and workflows
- Collective Unconscious PRD: Cross-project pattern sharing specification
- OpenClaw-RL Research: Trajectory tracking and OPD patterns
- Mem0 Documentation: Alternative memory system comparison

### 10.6 Revision History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-03-16 | Hydra Team | Initial PRD creation |
| 1.1 | 2026-03-16 | Hydra Team | Validated and atomicity-compliant version |
| 2.0 | 2026-03-17 | Hydra Team | Enriched with continuous discovery, OpenClaw-RL patterns, 4-phase implementation |
| 2.1 | 2026-03-18 | Hydra Team | Converted to PRD skill format with atomic requirements (max 3 ACs each) |

---

## 11. Next Steps

### Immediate Actions
1. Review enriched PRD with stakeholders
2. Validate technical approach with architecture team
3. Create feature branch: `feature/hindsight-integration`
4. Begin Phase 1 implementation (Foundation)

### Phase 1 Deliverables (Weeks 1-2)
- Hindsight server lifecycle management
- Go client implementation
- Configuration system integration
- 4 integration points operational
- Basic recall/retain working

### Phase 2 Preparation
- Design context-processor agent prompt
- Define tagging taxonomy
- Plan continuous discovery service architecture
- Prepare A/B testing framework design

---

**End of Document**

**Total Requirements:**  
- Functional: 58 (FR-001a to FR-029b, auto-split for atomicity)  
- Non-Functional: 8 (NFR-001 to NFR-008)  
- User Stories: 12 (US-001 to US-012)  

**Implementation Estimate:** 8 weeks across 4 phases  
**Risk Level:** Medium (proven patterns, gradual rollout)  
**Expected ROI:** 40% time reduction, 30% pattern reuse, 75% one-shot success

---
### 5.8 Implementation Mapping

| Requirement | Files | Integration Points | Data Models |
|-------------|-------|-------------------|-------------|
| US-005 | internal/hindsight/client.go, config/hindsight.go | - | - |
| US-006 | agents/context-processor.go | Point 1 | ContextResult |
| US-007 | agents/consensus-facilitator.go | - | ConsensusVote, ConsensusResult |
| US-008 | services/discovery.go | - | - |
| US-009 | internal/hindsight/metadata.go | - | - |
| US-010 | services/ab-testing.go | - | - |
| US-011 | services/failure-analysis.go | - | - |
| US-012 | dashboard/expertise.go | - | - |
| FR-001a | internal/hindsight/server.go | - | - |
| FR-001b | internal/hindsight/server.go | - | - |
| FR-002a | internal/hindsight/client.go | - | - |
| FR-002b | internal/hindsight/client.go | - | - |
| FR-003a | config/intelligence.go | - | - |
| FR-003b | config/intelligence.go | - | - |
| FR-004a | overseer.go | Point 1 | - |
| FR-004b | overseer.go | Point 1 | - |
| FR-005a | worker/main.go | - | - |
| FR-005b | worker/main.go | - | - |
| FR-006a | worker/main.go | Point 2 | ExecutionMemory |
| FR-006b | worker/main.go | Point 2 | ExecutionMemory |
| FR-007a | worker/main.go | Point 2 | ExecutionMemory |
| FR-007b | worker/main.go | Point 2 | ExecutionMemory |
| FR-008a | pattern_review.go | Point 3 | - |
| FR-008b | pattern_review.go | Point 3 | - |
| FR-009a | settled_state_service.go | Point 4 | - |
| FR-009b | settled_state_service.go | Point 4 | - |
| FR-010a | internal/hindsight/client.go | - | - |
| FR-010b | internal/hindsight/client.go | - | - |
| FR-011a | agents/context-processor.go | Point 1 | ContextResult |
| FR-011b | agents/context-processor.go | Point 1 | ContextResult |
| FR-012a | agents/context-processor.go | Point 1 | ContextResult |
| FR-012b | agents/context-processor.go | Point 1 | ContextResult |
| FR-013a | agents/context-processor.go | Point 1 | ContextResult |
| FR-013b | agents/context-processor.go | Point 1 | ContextResult |
| FR-014a | agents/context-processor.go | Point 1 | ContextResult |
| FR-014b | agents/context-processor.go | Point 1 | ContextResult |
| FR-015a | agents/context-processor.go, internal/hindsight/client.go | Point 1 | ContextResult |
| FR-015b | agents/context-processor.go, internal/hindsight/client.go | Point 1 | ContextResult |
| FR-016a | services/discovery.go | - | - |
| FR-016b | services/discovery.go | - | - |
| FR-017a | internal/hindsight/metadata.go | - | - |
| FR-017b | internal/hindsight/metadata.go | - | - |
| FR-018a | services/ab-testing.go | - | - |
| FR-018b | services/ab-testing.go | - | - |
| FR-019a | agents/consensus-facilitator.go | - | ConsensusVote, ConsensusResult |
| FR-019b | agents/consensus-facilitator.go | - | ConsensusVote, ConsensusResult |
| FR-020a | agents/consensus-facilitator.go | - | ConsensusVote |
| FR-020b | agents/consensus-facilitator.go | - | ConsensusVote |
| FR-021a | agents/consensus-facilitator.go | - | ConsensusVote |
| FR-021b | agents/consensus-facilitator.go | - | ConsensusVote |
| FR-022a | agents/consensus-facilitator.go, internal/hindsight/client.go | - | ConsensusResult |
| FR-022b | agents/consensus-facilitator.go, internal/hindsight/client.go | - | ConsensusResult |
| FR-023a | services/failure-analysis.go | - | - |
| FR-023b | services/failure-analysis.go | - | - |
| FR-024a | services/retention.go | - | - |
| FR-024b | services/retention.go | - | - |
| FR-024c | services/retention.go | - | - |
| FR-025a | dashboard/expertise.go | - | - |
| FR-025b | dashboard/expertise.go | - | - |
| FR-026a | internal/hindsight/llm.go | - | - |
| FR-026b | internal/hindsight/llm.go | - | - |
| FR-027a | internal/hindsight/storage.go | - | - |
| FR-027b | internal/hindsight/storage.go | - | - |
| FR-028a | internal/hindsight/logger.go | - | - |
| FR-028b | internal/hindsight/logger.go | - | - |
| FR-029a | internal/intelligence/cass_memory.go | - | - |
| FR-029b | internal/intelligence/cass_memory.go | - | - |
| NFR-001 | internal/hindsight/client.go, config/intelligence.go | - | - |
| NFR-002 | internal/hindsight/server.go | - | - |
| NFR-003 | internal/hindsight/storage.go | - | - |
| NFR-004 | services/retention.go | - | - |
| NFR-005 | internal/hindsight/client.go | - | - |
| NFR-006 | internal/hindsight/client.go, worker/main.go | Point 1, Point 2 | - |
| NFR-007 | config/intelligence.go | - | - |
| NFR-008 | pattern_review.go | Point 3 | - |
