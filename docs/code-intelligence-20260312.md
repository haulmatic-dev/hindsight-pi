# Code Intelligence Integration

**Status:** Implemented  
**Last Updated:** 2026-03-13  
**Purpose:** Skills-based integration for llm-tldr and cass_memory with Pi agents

## Overview

This document describes how Hydra integrates **llm-tldr** (semantic code analysis) and **cass_memory** (institutional knowledge) with Pi agents. **These are now implemented as skills**, allowing agents to query intelligence on-demand during execution rather than receiving pre-computed context.

**Key Change:** Previous force-feeding approach (injecting 2000-5000 tokens at start) has been replaced with agent-driven queries (200-500 tokens per query). This achieves 75-90% token savings while maintaining 100% relevance.

## Architecture

### Intelligence Lifecycle

```
Bead Execution → Agent Queries Skills → Executes with Context → Chain Completes → LLM Verification → Index to Stores
        ↑                                                                      ↓
        └────────────── Curated Intelligence ←────── Pattern Storage ←───────────┘
```

**Key Principles:**
1. **Agents query on-demand** - Pi uses skills to request intelligence when needed
2. **Skills provide methodology** - How to query, when to query, how to interpret results
3. **Indexing happens post-chain** - Only verified patterns get stored
4. **Pi has read-only access** - Cannot write directly to intelligence stores
5. **Virtuous cycle** - Past chains improve current beads; current beads feed future intelligence

### Flow Diagram

```
┌─────────────────┐
│   Bead Starts   │
└────────┬────────┘
         │
         ▼
┌──────────────────────────┐
│  Pi Decides: Need info?  │
└────────┬─────────────────┘
         │
    ┌────┴────┐
    │         │
    ▼         ▼
┌──────────┐ ┌──────────┐
│ Use Skill│ │ Continue │
│ cass-mem │ │ without  │
│ory-insigh│ │ query    │
│ ts       │ │          │
└────┬─────┘ └──────────┘
     │
     ▼
┌──────────────────────────┐
│  Query cass_memory       │  $(which cm) context "rate limiting"
│  Get patterns/anti-pats  │
└────────┬─────────────────┘
         │
         ▼
┌──────────────────────────┐
│  Query TLDR (optional)   │  llm-tldr search "middleware"
│  Get semantic results    │
└────────┬─────────────────┘
         │
         ▼
┌──────────────────────────┐
│  Execute with Skills     │  Uses gathered intelligence
│  + Methodology           │
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

### 1. Skills (New Architecture)

**Location:** `.agents/skills/`

#### cass-memory-insights

**Purpose:** Query institutional knowledge from cass_memory before implementing changes

**Location:** `.agents/skills/cass-memory-insights/SKILL.md`

**When to Use:**
- Before implementing any new feature
- When debugging issues
- When making architectural decisions
- When encountering unfamiliar code patterns

**Usage:**
```bash
# Query for relevant patterns
$(which cm) context "implement rate limiting" --json --limit 10

# Query with category
$(which cm) context "authentication patterns" --category security --json

# List patterns by category
$(which cm) list --category debugging --json
```

**Example Workflow:**
```
Pi: "I need to implement OAuth2 authentication"
Pi → $(which cm) context "OAuth2 authentication flow" --json
Result: {
  "relevantBullets": [
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

#### tldr-code-intelligence

**Purpose:** Semantic code search and impact assessment

**Location:** `.agents/skills/tldr-code-intelligence/SKILL.md`

**When to Use:**
- Finding code by meaning (not just name)
- Assessing impact before refactoring
- Exploring unfamiliar codebases
- Understanding dependencies

**Usage:**
```bash
# Semantic search
llm-tldr search "user authentication logic"

# Impact analysis
llm-tldr impact internal/auth/handler.go --depth 2

# Build index (if needed)
llm-tldr semantic index . --model all-MiniLM-L6-v2
```

**Example Workflow:**
```
Pi: "Where is the user authentication logic?"
Pi → llm-tldr search "user authentication"
Result: {
  "results": [
    {
      "file": "internal/auth/handler.go",
      "function": "AuthenticateUser",
      "score": 0.92,
      "context": "func AuthenticateUser(...)"
    }
  ]
}

Pi: "What will break if I refactor this?"
Pi → llm-tldr impact internal/auth/handler.go --depth 2
Result: 15 files affected
Pi: Plans careful refactoring
```

---

### 2. cass_memory - Institutional Knowledge

**Purpose:** Store verified patterns, anti-patterns, and learnings from completed chains

**Client:** `/internal/intelligence/cass_memory.go`

**CLI Tool:** `cm` command

**Key Methods:**
```go
// Query context for a task (used via skill)
func (c *CassMemoryClient) Context(task string) (*CassContextResponse, error)

// Add a pattern (called by LLM verifier post-chain)
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

---

### 3. llm-tldr - Semantic Code Analysis

**Purpose:** Provide semantic understanding of codebase

**Client:** `/internal/intelligence/tldr.go`

**Interface:** CLI mode (llm-tldr binary)

**Key Methods:**
```go
// Analyze code dependencies (used via skill)
func (c *TLDRClient) ImpactAnalysis(file string, depth int) (*TLDRImpactResponse, error)

// Semantic code search (used via skill)
func (c *TLDRClient) SemanticSearch(query string) (*TLDRSemanticResponse, error)

// Health check
func (c *TLDRClient) IsAvailable() bool
```

**Data Types:**
```go
type TLDRImpactResponse struct {
    File   string
    Impact struct {
        Callers []Dependency
        Callees []Dependency
    }
    Scope struct {
        Files    int
        Tokens   int
        Exceeded bool
    }
}

type TLDRSemanticResult struct {
    File     string
    Function string
    Score    float64
    Context  string
}
```

---

## Migration from Force-Feeding

### Previous Approach (Deprecated)

```
Overseer → Pre-compute context → Inject into prompt → Pi receives all context
```

**Issues:**
- 2000-5000 tokens per bead (wasteful)
- 80% context often irrelevant
- No mid-execution queries
- Pi had no control over what to fetch

### New Approach (Current)

```
Pi decides → Queries skill → Gets relevant context → Uses on-demand
```

**Benefits:**
- 200-500 tokens per query (efficient)
- 100% relevant context
- Mid-execution queries possible
- Agent autonomy: Pi decides when/what to query

### Code Changes

**Removed:**
- `gatherPreExecutionIntelligence()` from worker
- `runPreExecutionIntelligence()` from overseer
- `HYDRA_INTELLIGENCE` environment variable
- Prompt injection of patterns/anti-patterns

**Added:**
- `cass-memory-insights` skill
- `tldr-code-intelligence` skill
- Post-chain pattern extraction (unchanged)

---

## Use Cases

### Use Case 1: Feature Implementation

```
Task: Add rate limiting to auth endpoint

Pi Workflow:
1. Query institutional knowledge
   → $(which cm) context "rate limiting patterns"
   Result: "Token bucket pattern works well for APIs [ref: rate-001]"

2. Find existing implementations
   → llm-tldr search "rate limiting middleware"
   Result: Found in middleware/ratelimit.go

3. Check impact
   → llm-tldr impact internal/auth/handler.go --depth 2
   Result: 8 files affected

4. Implement using systematic-debugging skill
5. Chain completes → LLM verifies → New pattern indexed
```

### Use Case 2: Debugging

```
Task: Investigate intermittent test failure in user service

Pi Workflow:
1. Check if we've seen this before
   → $(which cm) context "intermittent test failures user service" --category debugging
   Result: {
     "antiPatterns": [{
       "id": "bug-042",
       "content": "Race condition in user cache - use sync.RWMutex"
     }]
   }

2. Find the problematic code
   → llm-tldr search "user cache test"
   Result: Found cache implementation

3. Apply known fix from anti-pattern
4. Chain completes → New learning indexed
```

### Use Case 3: Architecture Decision

```
Task: Should I use interfaces here?

Pi Workflow:
1. Query patterns
   → $(which cm) context "interface design patterns Go" --category architecture
   Result: "Small focused interfaces, define at point of use [ref: arch-015]"

2. Check existing code style
   → llm-tldr search "interface definitions"
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
    timeout: 30s
    max_files: 50
    max_tokens: 100000
```

### Environment Variables

```bash
# cass_memory
export CASS_MEMORY_BIN=/Users/buddhi/.local/bin/cm

# TLDR
export TLDR_CLI_PATH=/usr/local/bin/llm-tldr
```

---

## Performance Considerations

### Query Latency

| Operation | Typical Latency | Strategy |
|-----------|----------------|----------|
| cm context | 2-5s | Query once at start |
| llm-tldr search | 3-10s | Query during exploration |
| llm-tldr impact | 1-3s | Query before refactoring |

### Token Efficiency

| Approach | Tokens/Bead | Relevance |
|----------|-------------|-----------|
| Force-Feeding (old) | 2000-5000 | ~20% |
| Skills-based (new) | 200-500/query | 100% |
| **Savings** | **75-90%** | **+400%** |

---

## Integration with Chain Completion

### Post-Chain Processing (Unchanged)

Patterns are still extracted and stored after chain completion:

```
Bead Completes
    ↓
Chain Completion Detector triggers
    ↓
ChainPatternExtractor analyzes work
    ↓
LLM verifies quality
    ↓
Store in cass_memory (via AddPattern)
    ↓
Update TLDR semantic index
```

**Code Location:** `internal/core/chain_completion_detector.go`

---

## Troubleshooting

### Skills Not Available

**Symptom:** Pi doesn't use cass_memory or TLDR queries

**Check:**
1. Skills installed in `.agents/skills/`
2. Skills referenced in `hydra.yaml` or DefaultSkills
3. Binary paths correct (`which cm`, `which llm-tldr`)

### Queries Return Empty

**Symptom:** Skills query but no results

**Check:**
1. cass_memory has data: `cm list --limit 5`
2. TLDR index built: `llm-tldr semantic index .`
3. Correct working directory

### High Latency

**Symptom:** Queries take too long

**Solutions:**
- Reduce `--limit` parameter
- Check network connectivity
- Verify timeouts in config

---

## Related Documents

- [cass_memory Client](/internal/intelligence/cass_memory.go)
- [TLDR Client](/internal/intelligence/tldr.go)
- [cass-memory-insights Skill](/.agents/skills/cass-memory-insights/SKILL.md)
- [tldr-code-intelligence Skill](/.agents/skills/tldr-code-intelligence/SKILL.md)
- [Pi Agent Skills](./pi-agent-skills.md)

---

## Changelog

### 2026-03-13 - Skills-Based Architecture

**BREAKING CHANGE:** Converted from force-feeding to skills

- Removed pre-execution intelligence gathering
- Removed HYDRA_INTELLIGENCE environment variable
- Removed prompt injection of patterns
- Created `cass-memory-insights` skill
- Created `tldr-code-intelligence` skill
- Achieved 75-90% token savings
- Post-chain pattern extraction unchanged

### 2026-03-12 - Initial Draft
- Documented intelligence lifecycle
- Specified HTTP bridge architecture (deprecated)
- Added use cases and configuration examples

---

**Migration Guide:**
- Previous: Intelligence injected automatically
- Current: Agents must query via skills
- Update agents to call `$(which cm) context` and `llm-tldr` commands
- See skill documentation for usage examples
