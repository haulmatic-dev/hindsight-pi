# Code Intelligence Integration

**Status:** Draft  
**Last Updated:** 2026-03-12  
**Purpose:** Integration guide for llm-tldr and cass_memory with Pi agents

## Overview

This document describes how Hydra integrates **llm-tldr** (semantic code analysis) and **cass_memory** (institutional knowledge) with Pi agents. Unlike skills that provide methodology, these systems provide **contextual intelligence** that agents query dynamically during execution.

## Architecture

### Intelligence Lifecycle

```
Bead Execution → Chain Completion → LLM Verification → Index to cass_memory/tldr
                     ↑                                           ↓
            [Pi queries read-only] ←────── Curated Intelligence ─────┘
```

**Key Principles:**
1. **Indexing happens post-chain** - Not during bead execution
2. **LLM verification acts as quality gate** - Only valuable patterns get stored
3. **Pi has read-only access** - Cannot write directly to intelligence stores
4. **Virtuous cycle** - Past chains improve current beads; current beads feed future intelligence

### Flow Diagram

```
┌─────────────────┐
│   Bead Starts   │
└────────┬────────┘
         │
         ▼
┌──────────────────────────┐
│  Pi Queries cass_memory  │  "Have we solved this before?"
│      (Read-Only)         │
└────────┬─────────────────┘
         │
         ▼
┌──────────────────────────┐
│  Pi Queries llm-tldr     │  "Where is this implemented?"
│      (Read-Only)         │
└────────┬─────────────────┘
         │
         ▼
┌──────────────────────────┐
│  Pi Executes with Skills │  Uses gathered intelligence
│    + Methodology         │
└────────┬─────────────────┘
         │
         ▼
┌──────────────────────────┐
│   Chain Completes        │
└────────┬─────────────────┘
         │
         ▼
┌──────────────────────────┐
│  LLM Verifies Quality    │  "Is this worth remembering?"
└────────┬─────────────────┘
         │
         ▼
┌──────────────────────────┐
│  Index to cass_memory    │  Store patterns, anti-patterns
│  Index to llm-tldr       │  Update semantic code index
└──────────────────────────┘
```

---

## Components

### 1. cass_memory - Institutional Knowledge

**Purpose:** Store verified patterns, anti-patterns, and learnings from completed chains

**Current Integration:**
- **Client:** `/Users/buddhi/source-code/personal/hydra/internal/intelligence/cass_memory.go`
- **CLI Tool:** `cm` command
- **Data Structure:** Bullets, anti-patterns, history snippets

**Key Methods:**
```go
// Query context for a task
func (c *CassMemoryClient) Context(task string) (*CassContextResponse, error)

// Add a pattern (called by LLM verifier, not Pi)
func (c *CassMemoryClient) AddPattern(pattern, category string) error

// Check availability
func (c *CassMemoryClient) IsEnabled() bool
```

**Data Types:**
```go
type CassContextResponse struct {
    Success bool
    Error   string
    Data    struct {
        RelevantBullets []Bullet  // Knowledge bullets with scores
        AntiPatterns    []Bullet  // Patterns to avoid
        HistorySnippets []Snippet // Historical context
    }
}

type Bullet struct {
    ID       string
    Content  string
    Category string   // e.g., "testing", "security", "debugging"
    Score    float64
    Tags     []string
}
```

**Category Mappings:**
```go
var agentTypeToCategory = map[string]string{
    "orchestrator":        "orchestration",
    "test-specialist":     "testing",
    "code-reviewer":       "review",
    "security-auditor":    "security",
    "bug-fixer":           "debugging",
    // ... 15+ mappings
}
```

### 2. llm-tldr - Semantic Code Analysis

**Purpose:** Provide semantic understanding of codebase (not just text search)

**Current Integration:**
- **Client:** `/Users/buddhi/source-code/personal/hydra/internal/intelligence/tldr.go`
- **Interface:** HTTP API (daemon) + CLI fallback
- **Default URL:** `http://127.0.0.1:8787`

**Key Methods:**
```go
// Analyze what code depends on or is affected by a file
func (c *TLDRClient) ImpactAnalysis(file string, depth int) (*TLDRImpactResponse, error)

// Semantic code search
func (c *TLDRClient) SemanticSearch(query string) (*TLDRSemanticResponse, error)

// Extract structured context from file
func (c *TLDRClient) Context(file string) (*TLDRContextResponse, error)

// Health check
func (c *TLDRClient) IsAvailable() bool
```

**Data Types:**
```go
// Impact analysis response
type TLDRImpactResponse struct {
    File   string
    Impact struct {
        Callers []Dependency  // Functions calling this file
        Callees []Dependency  // Functions called by this file
    }
    Scope struct {
        Files    int
        Tokens   int
        Exceeded bool  // True if exceeds max_tokens
    }
}

type Dependency struct {
    File     string
    Function string
    Line     int
}

// Semantic search results
type TLDRSemanticResult struct {
    File     string
    Function string
    Score    float64  // Relevance score [0,1]
    Context  string   // Function signature or context
}
```

---

## Pi Tool Integration

### Exposed Tools (Read-Only)

Pi agents can call these tools during execution to query intelligence:

#### 1. cass_insight

**Purpose:** Query verified patterns from project history

**Parameters:**
```json
{
  "query": "authentication patterns",
  "category": "security",
  "limit": 5
}
```

**Usage Example:**
```
Pi: "I need to implement OAuth2 authentication"
Pi → cass_insight("OAuth2 patterns", "security", 5)
Result: {
  "patterns": [
    {
      "id": "auth-impl-001",
      "content": "Use JWT with refresh tokens for OAuth2",
      "category": "security",
      "score": 0.95
    }
  ],
  "antiPatterns": [
    {
      "id": "auth-ant-003",
      "content": "DON'T: Store tokens in localStorage",
      "category": "security"
    }
  ]
}
Pi: Uses JWT pattern as starting point
```

#### 2. tldr_context

**Purpose:** Semantic code search across codebase

**Parameters:**
```json
{
  "query": "user service implementation",
  "file": "internal/user/service.go",
  "max_results": 5
}
```

**Usage Example:**
```
Pi: "Where is the user authentication logic?"
Pi → tldr_context("user authentication", "", 5)
Result: {
  "results": [
    {
      "file": "internal/auth/handler.go",
      "function": "AuthenticateUser",
      "score": 0.92,
      "context": "func AuthenticateUser(ctx context.Context, req *LoginRequest) (*Token, error)"
    }
  ]
}
```

#### 3. tldr_impact

**Purpose:** Analyze code dependencies and impact scope

**Parameters:**
```json
{
  "file": "internal/auth/handler.go",
  "depth": 3
}
```

**Usage Example:**
```
Pi: "What will break if I refactor the auth handler?"
Pi → tldr_impact("internal/auth/handler.go", 3)
Result: {
  "callers": [
    {"file": "api/routes.go", "function": "SetupRoutes", "line": 45},
    {"file": "middleware/auth.go", "function": "AuthMiddleware", "line": 23}
  ],
  "callees": [
    {"file": "internal/auth/jwt.go", "function": "GenerateToken", "line": 67},
    {"file": "internal/user/service.go", "function": "GetUser", "line": 112}
  ],
  "scope": {
    "files": 15,
    "tokens": 12500,
    "exceeded": false
  }
}
Pi: "15 files affected - need careful refactoring plan"
```

---

## Implementation Options

### Option A: HTTP Bridge (Recommended)

**Architecture:**
```
Pi Extension → HTTP Call → Hydra API → cass_memory/tldr Client
```

**Pros:**
- Structured, type-safe
- Reuses existing Hydra infrastructure
- Easy to version and extend
- Can add authentication/rate limiting

**Implementation:**
```go
// Hydra exposes these endpoints:
GET /api/intelligence/cass?query=auth&category=security&limit=5
GET /api/intelligence/tldr/search?query=user+auth&max_results=5
GET /api/intelligence/tldr/impact?file=internal/auth/handler.go&depth=3

// Pi extension calls these via fetch/axios
```

### Option B: Direct CLI Access

**Architecture:**
```
Pi (bash tool) → cm command / lsp-cli
```

**Pros:**
- Simpler, no HTTP layer
- Uses existing CLI tools
- No build step for extensions

**Cons:**
- Less structured output
- Harder to version
- No central control

**Implementation:**
```bash
# Pi would run:
cm context "authentication" --json
lsp-cli semantic search "user auth"
```

### Recommendation: Option A (HTTP Bridge)

Better long-term maintainability and allows Hydra to control access patterns.

---

## Use Cases

### Use Case 1: Feature Implementation

```
Task: Add rate limiting to auth endpoint

Pi Workflow:
1. Query institutional knowledge
   → cass_insight("rate limiting patterns")
   Result: "Token bucket pattern works well for APIs [ref: rate-001]"

2. Find existing implementations
   → tldr_context("rate limiting middleware")
   Result: Found in middleware/ratelimit.go

3. Check impact
   → tldr_impact("internal/auth/handler.go", 2)
   Result: 8 files affected

4. Implement using systematic-debugging skill
5. Chain completes → LLM verifies → New pattern indexed
```

### Use Case 2: Debugging

```
Task: Investigate intermittent test failure in user service

Pi Workflow:
1. Check if we've seen this before
   → cass_insight("intermittent test failures user service", "debugging")
   Result: {
     "antiPatterns": [{
       "id": "bug-042",
       "content": "Race condition in user cache - use sync.RWMutex"
     }]
   }

2. Find the problematic code
   → tldr_context("user cache test", "internal/user/", 3)
   Result: Found cache implementation

3. Apply known fix from anti-pattern
4. Verify with tddr_impact to ensure no regressions
5. Chain completes → New learning indexed
```

### Use Case 3: Architecture Decision

```
Task: Should I use interfaces here?

Pi Workflow:
1. Query patterns
   → cass_insight("interface design patterns Go", "architecture")
   Result: "Small focused interfaces, define at point of use [ref: arch-015]"

2. Check existing code style
   → tldr_context("interface definitions")
   Result: Found patterns in 5 files

3. Consistent with established patterns
4. Implement following skill guidelines
```

---

## Configuration

### hydra.yaml

```yaml
intelligence:
  cass_memory:
    enabled: true
    query_limit: 10
    history_days: 30
    timeout: 5s
    
  tldr:
    enabled: true
    base_url: "http://127.0.0.1:8787"
    timeout: 10s
    max_files: 50
    max_tokens: 100000
    
  pi_tools:
    enabled: true
    # Which tools to expose to Pi agents
    tools:
      - cass_insight
      - tldr_context
      - tldr_impact
    # Cache settings for repeated queries
    cache:
      enabled: true
      ttl: 5m
```

### Environment Variables

```bash
# cass_memory
export CASS_MEMORY_BIN=/Users/buddhi/.local/bin/cm

# TLDR
export TLDR_BASE_URL=http://127.0.0.1:8787
export TLDR_CLI_PATH=/usr/local/bin/llm-tldr
```

---

## Integration with Existing Hydra Flow

### Pre-Execution Intelligence (Already Implemented)

**Current Flow:** `overseer.go` lines 660-787

```go
// runPreExecutionIntelligence gathers context before bead execution
func (o *Overseer) runPreExecutionIntelligence(bead *types.Bead) *IntelligenceBundle {
    bundle := &IntelligenceBundle{}
    
    // 1. Query cass_memory for context (if enabled)
    if o.cassMemory.IsEnabled() {
        context, err := o.cassMemory.Context(bead.Title)
        if err == nil {
            bundle.ContextEntries = context.Data.RelevantBullets
        }
    }
    
    // 2. Analyze impact with TLDR (if enabled and files available)
    if o.tldr.IsAvailable() && len(bead.Files) > 0 {
        for _, file := range bead.Files {
            impact, err := o.tldr.ImpactAnalysis(file, 2)
            if err == nil {
                bundle.ImpactAnalysis = impact
                break
            }
        }
    }
    
    // 3. Serialize to JSON and pass to worker
    bundleJSON, _ := json.Marshal(bundle)
    os.Setenv("HYDRA_INTELLIGENCE_BUNDLE", string(bundleJSON))
    
    return bundle
}
```

### Enhancement: Expose as Callable Tools

**Current:** Intelligence is injected once at bead start (force-feeding)

**Proposed:** Intelligence is queryable throughout execution (agent-driven)

**Implementation:**
```go
// Add to PiConnector or create IntelligenceService

type IntelligenceService struct {
    cassMemory *intelligence.CassMemoryClient
    tldr       *intelligence.TLDRClient
}

// Exposed as HTTP endpoints for Pi tools
func (s *IntelligenceService) HandleCassInsight(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query().Get("query")
    category := r.URL.Query().Get("category")
    
    context, err := s.cassMemory.Context(query)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    
    // Filter by category if provided
    json.NewEncoder(w).Encode(context.Data)
}

func (s *IntelligenceService) HandleTLDRSearch(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query().Get("query")
    
    results, err := s.tldr.SemanticSearch(query)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    
    json.NewEncoder(w).Encode(results)
}
```

---

## Quality Assurance Flow

### LLM Verification (Post-Chain)

**Purpose:** Ensure only valuable patterns get indexed

**Flow:**
```
Bead Completes
    ↓
LLM Evaluates Quality
    ↓
Worthy? → Index to cass_memory
        → Index code to llm-tldr
    
Not Worthy? → Discard
           → Log for analysis
```

**Criteria for Indexing:**
- Novel solution (not in existing patterns)
- Successful outcome (tests pass, no regressions)
- Generalizable (applies to similar tasks)
- Well-documented (clear explanation)

**Implementation:**
```go
// After chain completion
func (o *Overseer) postChainProcessing(bead *types.Bead, result *BeadResult) {
    if !result.Success {
        return  // Don't index failures
    }
    
    // LLM evaluates if worth indexing
    worthIndexing := o.llmEvaluateWorth(bead, result)
    
    if worthIndexing {
        // Extract patterns
        pattern := o.extractPattern(bead, result)
        o.cassMemory.AddPattern(pattern, bead.Category)
        
        // Update code index
        o.tldr.BuildIndex()
    }
}
```

---

## Performance Considerations

### Query Latency

| Operation | Typical Latency | Acceptable for Pi? |
|-----------|----------------|-------------------|
| cass_insight | 2-5s | Yes (strategic query) |
| tldr_context | 3-10s | Yes (understanding phase) |
| tldr_impact | 1-3s | Yes (planning phase) |

**Mitigations:**
- Cache frequent queries (5-minute TTL)
- Parallel queries when possible
- Async pre-fetch for known patterns

### Token Efficiency

**Force-Feeding (Current):**
- Injects all context upfront
- ~2000-5000 tokens per bead
- 80% may be irrelevant

**Agent-Driven (Proposed):**
- Pi queries only when needed
- ~200-500 tokens per query
- 100% relevant

**Savings:** 75-90% reduction in token usage

---

## Maintenance

### Monitoring

Track these metrics:
- Query frequency per tool
- Cache hit rates
- Average query latency
- Token savings vs force-feeding

### Index Updates

**Automatic:**
- llm-tldr index updates post-chain (if quality passes)
- cass_memory updates via LLM verification

**Manual:**
```bash
# Rebuild TLDR index
llm-tldr semantic index . --model all-MiniLM-L6-v2

# View cass_memory contents
cm list --category debugging
```

### Cleanup

**Periodic Tasks:**
- Archive old cass_memory entries (>90 days)
- Prune low-quality patterns
- Compact TLDR index

---

## Related Documents

- [cass_memory Client](/Users/buddhi/source-code/personal/hydra/internal/intelligence/cass_memory.go)
- [TLDR Client](/Users/buddhi/source-code/personal/hydra/internal/intelligence/tldr.go)
- [Overseer Intelligence Flow](/Users/buddhi/source-code/personal/hydra/internal/core/overseer.go#L660)
- [Pi Agent Skills](./pi-agent-skills.md)
- [Hydra Agent Connector Architecture](./architecture/agent-connector-20260311.md)

---

## Changelog

### 2026-03-12 - Initial Draft
- Documented intelligence lifecycle (post-chain indexing)
- Defined read-only tools for Pi agents
- Specified HTTP bridge architecture
- Added use cases and configuration examples
- Documented quality assurance flow

---

**Next Steps:**
1. Implement HTTP bridge for Pi tool access
2. Create Pi extensions/skills for intelligence queries
3. Add monitoring and metrics
4. Test with real beads
