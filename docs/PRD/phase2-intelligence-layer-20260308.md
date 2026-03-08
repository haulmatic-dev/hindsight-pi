# Product Requirements Document: Phase 2 - Intelligence Layer

**Version:** 1.0  
**Date:** 2026-03-08  
**Status:** Ready for Implementation  
**Based on:** Research from 3 specialized agents

---

## 1. Executive Summary

Phase 2 adds an **Intelligence Layer** to Hydra that enables pre-execution analysis and post-execution learning. This layer integrates with external intelligence systems (cass_memory, llm-tldr, GPTCache) to provide workers with contextual knowledge, validate task scope, and continuously improve task execution through pattern learning.

### Business Value

- **Reduced Costs:** GPTCache reduces LLM API costs by 90%
- **Faster Execution:** Cached responses are 10-100x faster
- **Higher Success Rates:** Pre-execution scope validation prevents failures
- **Continuous Learning:** Successful patterns are recorded for future reuse
- **Better Context:** Workers receive relevant historical patterns before execution

---

## 2. User Stories

### US-001: Query Historical Patterns
**As a** worker process  
**I want** to query cass_memory for relevant patterns before execution  
**So that** I can learn from previous similar tasks

**Acceptance Criteria:**
- Query `cm context` with bead description
- Receive relevant bullets, anti-patterns, and history
- Parse JSON response with proper error handling
- Fallback gracefully if cass_memory unavailable

### US-002: Record Successful Patterns
**As a** system  
**I want** to record successful execution patterns to cass_memory  
**So that** future workers can learn from them

**Acceptance Criteria:**
- Record pattern with `cm add` after successful completion
- Categorize by agent type (orchestrator, test-specialist, etc.)
- Record outcome with `cm outcome` including rule IDs
- Handle errors without blocking task completion

### US-003: Pre-Execution Impact Analysis
**As an** overseer  
**I want** to analyze code impact before spawning workers  
**So that** I can validate task scope and prevent scope violations

**Acceptance Criteria:**
- Call `tldr_impact` for target files
- Check scope_ceiling.files against limits
- Check scope_ceiling.tokens against model limits
- Block tasks exceeding scope and create breakdown tasks

### US-004: Semantic Code Search
**As a** worker  
**I want** to search codebase by concept using natural language  
**So that** I can find relevant code without knowing exact locations

**Acceptance Criteria:**
- Query `tldr_semantic` with bead description
- Receive matching files with relevance scores
- Filter results by score threshold
- Include context snippets in worker bundle

### US-005: LLM Response Caching
**As a** system  
**I want** to cache LLM responses via GPTCache  
**So that** I can reduce API costs and improve response times

**Acceptance Criteria:**
- Check GPTCache before LLM API calls
- Store successful responses in cache
- Track cache hit rates and metrics
- Handle cache misses gracefully

### US-006: Scope Validation
**As an** overseer  
**I want** to validate task scope against intelligent limits  
**So that** I prevent tasks that are too large to complete successfully

**Acceptance Criteria:**
- Define configurable scope limits (files, tokens)
- Validate against TLDR impact analysis
- Create child tasks for oversized work
- Log scope violations with details

---

## 3. Functional Requirements

### FR-001: Cass Memory Client

**Description:** Go client wrapper for cass_memory CLI commands.

**Requirements:**
- Create `internal/intelligence/cass_memory.go`
- Implement `Context(task string) (*ContextResponse, error)`
- Implement `AddPattern(pattern, category string) error`
- Implement `RecordOutcome(status string, ruleIDs []string, duration time.Duration) error`
- Support JSON output parsing
- Map agent types to cass_memory categories

**Data Structures:**
```go
type CassMemoryClient struct {
    enabled  bool
    timeout  time.Duration
}

type ContextResponse struct {
    RelevantBullets   []Bullet
    AntiPatterns      []Bullet
    HistorySnippets   []Snippet
    SuggestedQueries  []string
}

type Bullet struct {
    ID          string
    Content     string
    Category    string
    Relevance   float64
}
```

**Category Mapping:**
| Agent Type | Category |
|------------|----------|
| orchestrator | orchestration |
| test-specialist | testing |
| code-reviewer | code-review |
| fix-specialist | debugging |

### FR-002: LLM-TLDR Client

**Description:** HTTP client for llm-tldr daemon API.

**Requirements:**
- Create `internal/intelligence/tldr.go`
- Implement `ImpactAnalysis(file string, depth int) (*ImpactResponse, error)`
- Implement `SemanticSearch(query string) (*SemanticResponse, error)`
- Implement `Context(file string) (*ContextResponse, error)`
- Support configurable base URL (default: http://localhost:3000)
- Parse JSON responses

**Data Structures:**
```go
type TLDRClient struct {
    baseURL string
    client  *http.Client
}

type ImpactResponse struct {
    File    string
    Impact  struct {
        Callers []Dependency
        Callees []Dependency
    }
    Scope   struct {
        Files    int
        Tokens   int
        Exceeded bool
    }
}

type SemanticResponse struct {
    Results []struct {
        File      string
        Function  string
        Score     float64
        Context   string
    }
}
```

### FR-003: GPTCache Client

**Description:** HTTP client for GPTCache server.

**Requirements:**
- Create `pkg/gptcache/client.go`
- Implement `Get(prompt string) (*CacheResult, error)`
- Implement `Put(prompt, answer string) error`
- Support configurable server URL
- Handle cache misses (return nil, no error)

**Data Structures:**
```go
type Client struct {
    baseURL string
    enabled bool
}

type CacheResult struct {
    Answer     string
    CacheHit   bool
    Similarity float64
}
```

### FR-004: Pre-Execution Intelligence Hook

**Description:** Overseer hook to run intelligence queries before spawning workers.

**Requirements:**
- Add `runPreExecutionIntelligence(bead *Bead) (*IntelligenceBundle, error)`
- Call TLDR impact analysis for target files
- Call cass_memory context query
- Validate scope against limits
- Return intelligence bundle to worker

**Workflow:**
1. Extract target files from bead description
2. Run `tldr_impact` for each file
3. Aggregate scope metrics
4. Validate against limits
5. Query `cm context` with bead description
6. Query `tldr_semantic` for additional context
7. Build intelligence bundle

**Scope Validation:**
```go
type ScopeLimits struct {
    MaxFiles  int
    MaxTokens int
}

func ValidateScope(impact *ImpactResponse, limits ScopeLimits) error
```

### FR-005: Post-Execution Learning Loop

**Description:** Record successful patterns after worker completion.

**Requirements:**
- Add `recordSuccessPattern(bead *Bead, output string)` to worker
- Call `cm add` with execution summary
- Call `cm outcome` with shown rule IDs
- Handle failures gracefully

**Recording Format:**
```
Pattern: "Successfully implemented [feature] using [pattern] in [language]"
Category: [agent_type]
Context: [brief context]
```

### FR-006: Intelligence Configuration

**Description:** Configuration options for intelligence systems.

**Requirements:**
- Add to `hydra.yaml`:
```yaml
intelligence:
  cass_memory:
    enabled: true
    query_limit: 10
    history_days: 30
    timeout: 10s
  
  tldr:
    enabled: true
    base_url: "http://localhost:3000"
    timeout: 10s
    max_files: 10
    max_tokens: 5000
  
  gptcache:
    enabled: true
    server_url: "http://localhost:8000"
    similarity_threshold: 0.8
```

### FR-007: Worker Integration

**Description:** Workers receive and use intelligence bundle.

**Requirements:**
- Pass intelligence bundle to worker via environment or file
- Worker queries GPTCache before LLM calls
- Worker applies relevant patterns from cass_memory
- Worker uses TLDR context for code understanding

**Environment Variables:**
```bash
HYDRA_CASS_CONTEXT='{"bullets": [...], "anti_patterns": [...]}'
HYDRA_TLDR_IMPACT='{"files": 5, "tokens": 2400}'
HYDRA_GPTCACHE_URL='http://localhost:8000'
```

---

## 4. Non-Functional Requirements

### NFR-001: Performance

- cass_memory context query: < 1s
- TLDR impact analysis: < 500ms per file
- GPTCache lookup: < 100ms
- Pre-execution hook total: < 3s

### NFR-002: Reliability

- Graceful degradation if intelligence systems unavailable
- Retry logic with exponential backoff
- Circuit breaker for external services
- Continue without intelligence if services down

### NFR-003: Scalability

- Support 100+ concurrent intelligence queries
- Cache TLDR results in overseer to share across workers
- Rate limit external API calls
- Connection pooling for HTTP clients

### NFR-004: Observability

- Metrics: intelligence_query_duration, cache_hit_rate, scope_violations
- Structured logging for all intelligence operations
- Trace IDs across intelligence systems
- Dashboard for monitoring

---

## 5. Technical Considerations

### Architecture

```
┌─────────────────────────────────────────┐
│              Overseer                    │
│  ┌──────────────────────────────────┐   │
│  │  Pre-Execution Intelligence Hook │   │
│  └──────────────┬───────────────────┘   │
└─────────────────┼───────────────────────┘
                  │
    ┌─────────────┼─────────────┐
    ▼             ▼             ▼
┌────────┐  ┌──────────┐  ┌──────────┐
│cass_mem│  │ llm-tldr │  │ GPTCache │
│  (CLI) │  │  (HTTP)  │  │  (HTTP)  │
└────────┘  └──────────┘  └──────────┘
```

### File Structure

```
internal/intelligence/
├── cass_memory.go       # Cass memory client
├── cass_memory_test.go
├── tldr.go              # TLDR client
├── tldr_test.go
└── scope_validator.go   # Scope validation logic

pkg/gptcache/
├── client.go            # GPTCache client
└── client_test.go

internal/core/
├── overseer.go          # Add intelligence hooks
└── config.go            # Add intelligence config
```

### Dependencies

**External Services:**
- cass_memory CLI (already installed)
- llm-tldr daemon (port 3000)
- GPTCache server (port 8000, optional Docker)

**Go Libraries:**
- Standard library only (net/http, encoding/json)

### Integration Points

1. **Overseer.tick()** - Call pre-execution hook before spawning
2. **Worker.main()** - Use intelligence bundle, record patterns
3. **Config.Load()** - Parse intelligence configuration
4. **API handlers** - Expose intelligence metrics

---

## 6. Risks and Mitigation

| Risk | Impact | Mitigation |
|------|--------|------------|
| cass_memory CLI unavailable | Medium | Degraded mode - continue without patterns |
| TLDR daemon not running | Medium | Skip impact analysis, spawn anyway |
| GPTCache server down | Low | Direct LLM calls, no caching |
| High latency on queries | Medium | Add timeouts, circuit breakers |
| Scope validation too strict | Medium | Configurable limits, override option |

---

## 7. MoSCoW Prioritization

### Must Have
- FR-001: Cass memory client
- FR-004: Pre-execution hook (basic)
- FR-005: Post-execution learning
- FR-006: Configuration

### Should Have
- FR-002: TLDR client with impact analysis
- FR-003: GPTCache client
- Scope validation and blocking

### Could Have
- TLDR semantic search integration
- Advanced caching strategies
- Custom embedding models

### Won't Have
- Custom LLM training
- Real-time collaboration features
- Distributed intelligence systems

---

## 8. Success Metrics

**Functional:**
- 100% of workers query cass_memory before execution
- 80%+ cache hit rate for GPTCache
- < 5% scope violations after validation
- Patterns recorded for 100% of successful tasks

**Performance:**
- Pre-execution hook: < 3s total
- cass_memory query: < 1s
- TLDR impact: < 500ms per file
- GPTCache lookup: < 100ms

**Quality:**
- Zero crashes from intelligence systems
- Graceful degradation in 100% of failure cases

---

## 9. Research Insights

### cass_memory
- CLI available at `/Users/buddhi/.local/bin/cm`
- Version 0.2.3, stable
- JSON output supported via `--json` flag
- Categories: orchestration, testing, code-review, debugging
- Query takes ~500-600ms

### llm-tldr
- HTTP API on port 3000
- 5-layer analysis (context, impact, semantic, callgraph, change)
- Embeddings-based semantic search
- Impact analysis returns callers, callees, scope metrics
- Provides 95% token savings

### GPTCache
- Python library, integrate via HTTP server
- Docker deployment available
- Semantic similarity matching (not just exact)
- 90% cost reduction, 10-100x speedup for cached queries
- Configurable similarity threshold (default 0.8)

---

## 10. Implementation Phases

### Phase 2A: Cass Memory (Week 1)
- Implement cass_memory client
- Add post-execution recording
- Add configuration
- Basic pre-execution hook

### Phase 2B: TLDR Integration (Week 2)
- Implement TLDR client
- Add impact analysis to pre-execution hook
- Add scope validation
- Test with real projects

### Phase 2C: GPTCache (Week 3)
- Implement GPTCache client
- Deploy GPTCache server (Docker)
- Integrate with worker LLM calls
- Add metrics and monitoring

### Phase 2D: Polish (Week 4)
- Performance optimization
- Error handling improvements
- Metrics dashboard
- Documentation

---

**Total Duration:** 4 weeks
**Estimated Effort:** ~1,200 lines of code + tests
