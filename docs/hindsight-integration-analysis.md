# Hindsight Integration Points Analysis

## 1. Planned Integration Points (from PRD)

### Core Operations Required by PRD:

| Operation | PRD Reference | Status | Implementation Location |
|-----------|---------------|--------|------------------------|
| **Recall** | US-001, FR-004a/b | ✅ Implemented | `internal/hindsight/client.go:Search()` |
| **Retain** | US-002, FR-006a/b | ✅ Implemented | `internal/hindsight/client.go:Retain()` |
| **Reflect** | US-004, FR-009a/b | ❌ NOT Implemented | **MISSING** |
| **Health** | NFR-002 | ✅ Implemented | `internal/hindsight/storage.go:Health()` |

### Integration Points (4 Total):

#### Point 1: Pre-Execution Context Retrieval
- **Location:** `overseer.go:spawnWorkerInternal()` (line ~2227)
- **Operation:** Recall/Search
- **Status:** ✅ Implemented
- **Details:** 
  - Queries Hindsight before worker spawn
  - Injects context via HYDRA_HISTORIESIGHT_CONTEXT env var
  - Worker parses and uses context in `cmd/worker/main.go:queryHindsightForContext()`

#### Point 2: Post-Execution Memory Retention
- **Location:** `worker/main.go:recordExecutionResult()` (line ~958)
- **Operation:** Retain
- **Status:** ✅ Implemented
- **Details:**
  - Stores execution results after bead completion
  - Includes: bead metadata, success/failure, output, duration, files
  - Fail-fast: triggers retry on retention failure

#### Point 3: Pattern Quality Enhancement
- **Location:** `pattern_review.go:calculateQualityScore()`
- **Operation:** Recall/Search (for historical patterns)
- **Status:** 🟡 Partially Implemented
- **Details:**
  - Should query Hindsight for similar patterns
  - Adjust quality score: `adjusted = base * 0.7 + success_rate * 0.3`
  - Currently uses internal quality gate service, not Hindsight

#### Point 4: Settled-State Reflection
- **Location:** `settled_state_service.go:settleBead()`
- **Operation:** **Reflect** (NOT available in current client)
- **Status:** ❌ NOT Implemented
- **Details:**
  - Should generate insights from settled beads
  - Calls `Reflect()` with bead ID and context
  - Stores insights as new memories with type "reflection_insight"
  - **BLOCKED:** Missing `Reflect` operation in Go client

## 2. Additional Features from PRD (Not Yet Implemented)

### Phase 2: Intelligence Features

| Feature | PRD Reference | Status | Notes |
|---------|---------------|--------|-------|
| **Context-Processor Agent** | US-006, FR-011-015 | 🟡 Partial | Agent exists but not fully integrated with overseer |
| **Continuous Discovery** | US-008, FR-016 | ❌ Not Implemented | Async background discovery when confidence < 0.40 |
| **Multi-Dimensional Tagging** | US-009, FR-017 | 🟡 Partial | Tag structure exists, not actively used |
| **A/B Prompt Testing** | US-010, FR-018 | ✅ Implemented | `internal/services/ab-testing.go` |
| **Multi-Agent Consensus** | US-007, FR-019-022 | ❌ Not Implemented | Facilitator + reviewers + voting |
| **Failure Mode Analysis** | US-011, FR-023 | ❌ Not Implemented | Query similar failures, suggest fixes |

### Phase 3-4: Advanced Features

| Feature | PRD Reference | Status | Notes |
|---------|---------------|--------|-------|
| **Utility-Weighted Retention** | US-012, FR-024 | ✅ Implemented | `internal/services/retention.go` |
| **Expertise Dashboard** | FR-025 | ❌ Not Implemented | Metrics visualization (out of scope) |
| **LLM Provider Abstraction** | FR-026 | ✅ Implemented | `internal/hindsight/llm.go` |

## 3. Hindsight API Operations

### Current Go Client Operations:

```go
// Available in internal/hindsight/client.go:
- Search(query, beadType string, limit int) (*SearchResponse, error)
- SearchByBead(title, beadType string) (*SearchResponse, error)
- Retain(memory *ExecutionMemory) (*RetentionResult, error)
- RetainContextResult(result *ContextProcessorResult) (*RetentionResult, error)
- IsEnabled() bool
```

### Missing Operations (available in Python Hindsight):

```python
# From hindsight Python library:
- reflect(bank_id, query=None, context=None) -> List[Insight]
  # Generates insights from existing memories
  
- create_bank(bank_id, name=None, description=None)
- set_mission(bank_id, mission)
- create_mental_model(bank_id, name, content)
- create_directive(bank_id, name, content)
```

## 4. Critical Gap: Reflect Operation

### Why It's Needed:
According to PRD Section 5.2 (Point 4):
> **Point 4: Settled-State Reflection** (`settled_state_service.go:settleBead()`)
> - Query: Bead ID + dependencies
> - Action: Generate and store insights

### What It Does:
From Hindsight documentation:
- Analyzes existing memories in a bank
- Generates new insights/mental models
- Identifies patterns across multiple memories
- Creates higher-level abstractions

### Implementation Required:

```go
// Need to add to internal/hindsight/client.go:

// Reflect generates insights from existing memories
func (c *Client) Reflect(beadID string, context map[string]interface{}) (*ReflectResponse, error) {
    // Call POST /api/v1/reflect
    // Request: { "bank_id": "hydra", "query": "...", "context": {...} }
    // Response: { "insights": [...], "mental_models": [...] }
}

type ReflectResponse struct {
    Success      bool      `json:"success"`
    Insights     []Insight `json:"insights"`
    MentalModels []Model   `json:"mental_models"`
}

type Insight struct {
    ID          string  `json:"id"`
    Content     string  `json:"content"`
    Confidence  float64 `json:"confidence"`
    SourceCount int     `json:"source_count"`
}
```

## 5. Implementation Priority

### High Priority (Blocks Core Functionality):
1. **Add Reflect operation to Go client** (FR-009)
   - Required for settled-state reflection
   - Enables insight generation from completed beads

2. **Integrate Reflect into SettledStateService** (FR-009)
   - Call Reflect() after bead settlement
   - Store generated insights in Hindsight

### Medium Priority (Enhancement):
3. **Complete Context-Processor Integration** (FR-011-015)
   - Currently agent exists but overseer doesn't invoke it
   - Need to add `spawnWorkerWithContextProcessor()` path

4. **Pattern Quality Enhancement with Hindsight** (FR-008)
   - Query Hindsight for similar patterns
   - Adjust quality scores based on historical success

### Low Priority (Nice-to-Have):
5. **Multi-Agent Consensus** (FR-019-022)
   - Complex feature for architectural decisions
   - Requires facilitator agent implementation

6. **Continuous Discovery Service** (FR-016)
   - Async background research
   - Triggered when confidence < 0.40

## 6. Current Integration Status

✅ **Working:**
- Hindsight server running on port 9418 with Kimi Coding API
- Overseer initializes Hindsight client
- Workers receive Hindsight config via env vars
- Pre-execution context retrieval (Recall/Search)
- Post-execution memory retention (Retain)
- A/B Testing framework
- Utility-weighted retention policies

❌ **Not Working:**
- Settled-state reflection (missing Reflect operation)
- Context-processor not invoked by overseer
- Pattern quality doesn't use Hindsight data
- Multi-agent consensus not implemented

## 7. Recommended Next Steps

1. **Implement Reflect operation** in `internal/hindsight/client.go`
2. **Add Reflect endpoint** to Hindsight server (if not exists)
3. **Integrate Reflect** into `settled_state_service.go`
4. **Test settled-state reflection** with a completed bead
5. **Verify insights** are being generated and stored

## 8. Files to Modify

### High Priority:
- `internal/hindsight/client.go` - Add Reflect method
- `internal/hindsight/types.go` - Add Reflect types
- `internal/core/settled_state_service.go` - Call Reflect

### Medium Priority:
- `internal/core/overseer.go` - Add context-processor invocation
- `internal/intelligence/pattern_review.go` - Query Hindsight

## Summary

**Current Status:** 60% Complete
- Core operations (Recall, Retain) ✅
- Server running ✅
- Integration points 1 & 2 ✅
- **Missing:** Reflect operation (Point 4) ❌
- **Missing:** Context-processor invocation (Point 3 enhancement) 🟡

**Critical Path:** Implement Reflect operation to enable settled-state reflection and complete the 4th integration point from the PRD.
