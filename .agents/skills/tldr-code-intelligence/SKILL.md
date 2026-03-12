---
name: tldr-code-intelligence
description: Semantic code analysis and impact assessment. Search code by meaning (not just text), analyze dependencies, and understand impact of changes before refactoring.
---

# TLDR Code Intelligence

## Overview

**TLDR** (Too Long; Didn't Read) provides semantic understanding of your codebase. Unlike text search (`grep`), TLDR understands code meaning and relationships.

**When to use:**
- Finding code by what it does (not what it's named)
- Assessing impact before refactoring
- Exploring unfamiliar codebases
- Understanding dependencies and call chains

**Key principle:** Semantic search > text search. Use TLDR when you need to understand code relationships.

## Commands

### Semantic Search

Search for code by meaning, not just name.

```bash
# Find code related to a concept (using wrapper for logging)
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "user authentication logic"

# Search with context
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "database connection pooling" --limit 5

# Or use the direct command (without logging)
llm-tldr search "rate limiting middleware"
```

**When to use:**
- "Find where user authentication happens"
- "Show me all error handling patterns"
- "Where is rate limiting implemented?"

### Impact Analysis

Understand what depends on a file and what it depends on.

```bash
# Basic impact analysis
llm-tldr impact internal/auth/handler.go

# Deeper analysis (more levels of dependencies)
llm-tldr impact internal/auth/handler.go --depth 3

# Show full call graph
llm-tldr impact internal/database/db.go --depth 2 --format json
```

**When to use:**
- "What will break if I refactor this?"
- "How many files use this function?"
- "What does this code depend on?"

### Build Semantic Index

Create or update the semantic index for the current project.

```bash
# Build index for current directory
llm-tldr semantic index .

# Build with specific model
llm-tldr semantic index . --model all-MiniLM-L6-v2

# Force rebuild (ignore cache)
llm-tldr semantic index . --force
```

**When to use:**
- First time using TLDR on a project
- After major code changes
- When search results seem outdated

### Get Context

Extract structured context from a specific file.

```bash
# Get overview of a file
llm-tldr context internal/user/service.go

# Get specific function context
llm-tldr context internal/user/service.go --function Authenticate
```

## Best Practices

### When to Use TLDR vs Text Search

| Task | Text Search (grep) | TLDR Semantic |
|------|-------------------|---------------|
| Find string literals | ✅ | ❌ |
| Find exact function name | ✅ | ✅ |
| Find by behavior | ❌ | ✅ |
| Understand dependencies | ❌ | ✅ |
| Assess refactoring impact | ❌ | ✅ |

### Workflow Integration

**BEFORE refactoring:**

```bash
# 1. Build/update index if needed
llm-tldr semantic index . --model all-MiniLM-L6-v2

# 2. Analyze impact
llm-tldr impact internal/old/module.go --depth 2

# 3. See results
# {
#   "callers": [...],
#   "callees": [...],
#   "scope": {"files": 15, "tokens": 12500}
# }

# 4. Plan refactoring based on impact
```

**WHEN exploring code:**

```bash
# 1. Search for what you're looking for
llm-tldr search "error handling middleware"

# 2. Get detailed context
llm-tldr context internal/middleware/error.go

# 3. Understand relationships
llm-tldr impact internal/middleware/error.go
```

### Constructing Good Search Queries

**DO: Describe behavior**
```bash
# ✅ Good - describes what code does
llm-tldr search "validates JWT tokens"

# ❌ Bad - just keywords
llm-tldr search "JWT validation"
```

**DO: Be specific but not too narrow**
```bash
# ✅ Good
llm-tldr search "database transaction rollback handling"

# ❌ Too broad
llm-tldr search "database"

# ❌ Too narrow
llm-tldr search "PostgreSQL transaction rollback with savepoints in user service"
```

**DO: Use natural language**
```bash
# ✅ Good
llm-tldr search "implements circuit breaker pattern for external API calls"

# ❌ Bad - keyword soup
llm-tldr search "circuit breaker api external"
```

## Common Workflows

### Refactoring Workflow

```
Task: Refactor authentication middleware

# Step 1: Check current implementation context
llm-tldr context internal/middleware/auth.go

# Step 2: Analyze impact (WHAT WILL BREAK?)
llm-tldr impact internal/middleware/auth.go --depth 2

# Output: 12 files affected, including critical paths

# Step 3: Search for similar implementations
llm-tldr search "middleware refactoring patterns"

# Step 4: Plan careful migration
# (proceed with refactoring knowing full impact)
```

### Feature Implementation Workflow

```
Task: Add rate limiting

# Step 1: Check if it already exists
llm-tldr search "rate limiting implementation"

# Result: Found in internal/middleware/ratelimit.go

# Step 2: Understand current implementation
llm-tldr context internal/middleware/ratelimit.go

# Step 3: Check where it's used
llm-tldr impact internal/middleware/ratelimit.go

# Step 4: Extend existing implementation
```

### Debugging Workflow

```
Task: Find source of intermittent failures

# Step 1: Search for error-related code
llm-tldr search "error handling timeout retry"

# Step 2: Analyze suspicious code
llm-tldr impact internal/service/client.go --depth 2

# Step 3: Find all callers
llm-tldr search "calls external API with timeout"

# Step 4: Trace data flow
```

## Interpreting Results

### Impact Analysis Output

```json
{
  "file": "internal/auth/handler.go",
  "impact": {
    "callers": [
      {"file": "api/routes.go", "function": "SetupRoutes", "line": 45},
      {"file": "middleware/auth.go", "function": "AuthMiddleware", "line": 23}
    ],
    "callees": [
      {"file": "internal/auth/jwt.go", "function": "GenerateToken", "line": 67}
    ]
  },
  "scope": {
    "files": 15,
    "tokens": 12500,
    "exceeded": false
  }
}
```

**Interpretation:**
- `callers`: Code that calls your target (will break if you change API)
- `callees`: Code your target calls (may break if behavior changes)
- `scope.files`: Total files in impact radius
- `scope.exceeded`: True if analysis hit token limits

### Semantic Search Output

```json
[
  {
    "file": "internal/auth/handler.go",
    "function": "AuthenticateUser",
    "score": 0.92,
    "context": "func AuthenticateUser(ctx context.Context, req *LoginRequest) (*Token, error)"
  }
]
```

**Interpretation:**
- `score`: Relevance (0-1, higher is better)
- `context`: Function signature or surrounding code
- Results sorted by semantic similarity

## Limitations

- Requires semantic index (run `llm-tldr semantic index` first)
- Best for compiled/typed languages
- Index can become stale (rebuild after major changes)
- Not a replacement for reading code (use with `read` tool)

## Related Skills

- **cass-memory-insights**: Query institutional knowledge and patterns
- **lsp-code-analysis**: Navigate code with LSP (go-to-definition, find references)
- **systematic-debugging**: Structured debugging methodology

## Examples

### Finding Implementation Examples

```bash
# Find authentication implementations
llm-tldr search "JWT token validation implementation"

# Find error handling patterns
llm-tldr search "centralized error handling middleware"

# Find database patterns
llm-tldr search "repository pattern implementation"
```

### Pre-Refactoring Analysis

```bash
# Before moving a function
llm-tldr impact internal/utils/helpers.go --depth 2

# Before changing an interface
llm-tldr impact internal/interfaces/store.go --depth 3

# Before renaming
llm-tldr impact internal/models/user.go --depth 2
```

### Understanding Dependencies

```bash
# What does this service depend on?
llm-tldr impact internal/service/order.go --depth 1

# What depends on this database layer?
llm-tldr impact internal/db/connection.go --depth 2
```
