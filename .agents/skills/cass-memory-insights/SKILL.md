---
name: cass-memory-insights
description: Query institutional knowledge from cass_memory before implementing changes. Access verified patterns, anti-patterns, and historical learnings from completed chains.
---

# CASS Memory Insights

## Overview

**CASS** (Collective Agent Shared Storage) is Hydra's institutional memory system. It stores verified patterns, anti-patterns, and learnings from completed bead chains.

**When to use:**
- Before implementing any new feature
- When debugging issues
- When making architectural decisions
- When encountering unfamiliar code patterns

**Key principle:** Query CASS *before* you start coding. Past solutions prevent repeated mistakes.

## Commands

### Get Context

Query CASS for relevant patterns and anti-patterns based on your current task.

```bash
# Query for general context about a task
cm context "implement rate limiting" --json --limit 10

# Query with specific category (security, testing, debugging, etc)
cm context "authentication patterns" --category security --json --limit 5

# Query and see raw output
$(which cm) context "database migration" --json
```

**Response format:**
```json
{
  "success": true,
  "data": {
    "relevantBullets": [
      {
        "id": "pattern-001",
        "content": "Use JWT with refresh tokens for OAuth2",
        "category": "security",
        "score": 0.95
      }
    ],
    "antiPatterns": [
      {
        "id": "anti-003",
        "content": "DON'T: Store tokens in localStorage",
        "category": "security"
      }
    ],
    "historySnippets": [...]
  }
}
```

### List Patterns

Browse all stored patterns by category.

```bash
# List all debugging patterns
cm list --category debugging --json

# List top 10 security patterns
cm list --category security --limit 10 --json
```

### Add Pattern (Post-Implementation)

After successfully implementing a solution, add it to CASS for future agents.

```bash
# Add a successful pattern
cm add "Use connection pooling with max 20 connections for database access" --category performance --json

# Record an anti-pattern (what NOT to do)
cm add "DON'T: Use global variables for request-scoped data" --category anti-pattern --json
```

## Best Practices

### Constructing Effective Queries

1. **Be specific but not too narrow**
   - ✅ "implement OAuth2 authentication"
   - ❌ "authentication" (too broad)
   - ❌ "OAuth2 with refresh tokens and PKCE" (too specific)

2. **Use natural language**
   - ✅ "rate limiting middleware"
   - ❌ "rate_limit middleware go"

3. **Include intent**
   - ✅ "debug memory leak in goroutines"
   - ❌ "memory leak"

### Workflow Integration

**BEFORE you start coding:**

```bash
# 1. Query CASS for context
$(which cm) context "<your task description>" --json --limit 5

# 2. Read relevant patterns
# 3. Check anti-patterns to avoid
# 4. Proceed with implementation
```

**Example workflow:**

```
Task: Add rate limiting to API endpoints

Pi: Querying CASS for relevant patterns...
$(which cm) context "rate limiting API endpoints" --json --limit 5

Result: {
  "relevantBullets": [
    {
      "content": "Token bucket pattern works well for APIs [ref: rate-001]",
      "category": "architecture"
    }
  ],
  "antiPatterns": [
    {
      "content": "DON'T: Rate limit based on IP alone (VPNs, mobile networks)",
      "category": "security"
    }
  ]
}

Pi: Using token bucket pattern, avoiding IP-only limiting
[proceeds with implementation]
```

## When NOT to Use

- When you need code navigation (use `lsp-code-analysis` skill)
- When you need semantic search (use `tldr-code-intelligence` skill)
- When the task is completely novel (no similar patterns exist)

## Categories

Common categories to query:

| Category | Use Case |
|----------|----------|
| `security` | Authentication, authorization, encryption |
| `testing` | Test patterns, mocking, assertions |
| `debugging` | Debugging techniques, common bugs |
| `performance` | Optimization, caching, concurrency |
| `architecture` | Design patterns, structure |
| `database` | Schema design, queries, migrations |

## Related Skills

- **tldr-code-intelligence**: For semantic code search and impact analysis
- **lsp-code-analysis**: For code navigation and LSP-based analysis
- **systematic-debugging**: For structured debugging methodology

## Examples

### Feature Implementation

```bash
# Before implementing authentication
$(which cm) context "OAuth2 authentication flow" --category security --json

# Check for common pitfalls
$(which cm) context "authentication security anti-patterns" --category security --json
```

### Debugging

```bash
# Before debugging a database issue
$(which cm) context "database connection pool exhaustion" --category debugging --json

# Look for similar issues
$(which cm) context "intermittent test failures" --category testing --json
```

### Architecture Decision

```bash
# Before choosing an approach
$(which cm) context "microservice communication patterns" --category architecture --json

$(which cm) context "interface design patterns" --category architecture --json
```
