---
name: code-search-and-intelligence
description: Query institutional knowledge with cass_memory and search code semantically with TLDR. This skill guides when to use which intelligence service and how to construct effective queries.
---

# Code Search and Intelligence

## Overview

This skill provides access to two complementary intelligence systems:

1. **cass_memory** - Global institutional knowledge across all projects (patterns, anti-patterns, historical learnings)
2. **TLDR** - Semantic code search within the current repository (understanding code by meaning, not just names)

**Key principle:** Always query intelligence *before* implementing. Past solutions prevent repeated mistakes.

## Commands

### Query Institutional Knowledge (cass_memory)

Query for patterns and anti-patterns before starting any work.

```bash
# Get context for your task
cass-memory context "<your exact task description>" --json

# Query with specific category
cass-memory context "authentication patterns" --category security --json

# List patterns by category
cass-memory list --category debugging --json
```

**When to use:**
- ✅ **ALWAYS** before implementing new features
- ✅ **ALWAYS** before fixing bugs
- ✅ **ALWAYS** before refactoring
- ✅ Before making architectural decisions

**What to look for:**
1. **Patterns** - Successful approaches from past tasks
2. **Anti-patterns** - Mistakes to avoid (check FIRST)
3. **Category-specific** insights

### Search Code Semantically (TLDR)

Search for code by what it does, not what it's named.

```bash
# Find code related to a concept
hydra-llm-tldr search "user authentication logic"

# Search with limit
hydra-llm-tldr search "database connection pooling" --limit 5

# Search in specific directory
hydra-llm-tldr search "rate limiting middleware" --path internal/middleware
```

**When to use:**
- ✅ Finding code by behavior (not filename)
- ✅ Exploring unfamiliar codebases
- ✅ Understanding dependencies

**Don't use when:**
- ❌ File path already specified
- ❌ Modifying known file
- ❌ Simple edits

### Analyze Impact (TLDR)

Understand what depends on code before changing it.

```bash
# Basic impact analysis
hydra-llm-tldr impact internal/auth/handler.go

# Deeper analysis
hydra-llm-tldr impact internal/auth/handler.go --depth 3

# Show full call graph
hydra-llm-tldr impact internal/database/db.go --depth 2 --format json
```

**When to use:**
- ✅ Before refactoring
- ✅ Before changing interfaces
- ✅ Before moving files

**CRITICAL:** Always run impact analysis before:
- Modifying public APIs
- Changing interfaces
- Renaming functions
- Moving files

## Decision Matrix

| Task Type | Query cass_memory? | Search TLDR? | Priority |
|-----------|-------------------|--------------|----------|
| New feature | ✅ Yes | ✅ Yes | High |
| Bug fix | ✅ Yes | ✅ Yes | High |
| Refactoring | ✅ Yes | ✅ Yes (impact) | Critical |
| Documentation | ✅ Yes | ❌ No | Medium |
| Code review | ✅ Yes | ✅ Yes | High |
| Simple typo | ❌ No | ❌ No | Low |
| Rename variable | ❌ No | ❌ No | Low |

## Quick Decision Tree

```
Starting new task
    ↓
Query cass_memory first (always)
    ↓
Need to find code?
    ↓ YES
Search TLDR → Check impact (if modifying)
    ↓
Implement with findings
```

## Workflow Examples

### Example 1: Add New API Endpoint

```bash
# Step 1: Check institutional knowledge
cass-memory context "REST API endpoint implementation" --json

# Step 2: Find existing patterns  
hydra-llm-tldr search "API endpoint handler implementation"

# Step 3: Implement following patterns
```

### Example 2: Fix Authentication Bug

```bash
# Step 1: Check for known auth issues
cass-memory context "authentication bug" --category security --json

# Step 2: Find auth implementation
hydra-llm-tldr search "user authentication logic"

# Step 3: Analyze impact before fixing
hydra-llm-tldr impact internal/auth/handler.go --depth 2

# Step 4: Fix with full context
```

### Example 3: Refactor Database Layer

```bash
# Step 1: Check for refactoring patterns
cass-memory context "database layer refactoring" --category architecture --json

# Step 2: Find database implementations
hydra-llm-tldr search "database access layer repository"

# Step 3: Check impact of each file
hydra-llm-tldr impact internal/db/connection.go --depth 2
hydra-llm-tldr impact internal/db/repository.go --depth 2

# Step 4: Plan and execute carefully
```

## Constructing Good Queries

### cass_memory Queries

**DO: Be specific and include intent**
```bash
# ✅ Good
"implement OAuth2 authentication with refresh tokens"
"debug memory leak in worker pool"

# ❌ Too broad
"authentication"
"memory"
```

**DO: Use natural language**
```bash
# ✅ Good
"rate limiting middleware implementation"

# ❌ Keyword soup
"rate_limit middleware go"
```

### TLDR Search Queries

**DO: Describe behavior, not keywords**
```bash
# ✅ Good - describes what code does
hydra-llm-tldr search "validates JWT tokens"

# ❌ Bad - just keywords
hydra-llm-tldr search "JWT validation"
```

**DO: Be specific but not too narrow**
```bash
# ✅ Good
hydra-llm-tldr search "database transaction rollback handling"

# ❌ Too broad
hydra-llm-tldr search "database"

# ❌ Too narrow
hydra-llm-tldr search "PostgreSQL transaction rollback with savepoints in user service"
```

**DO: Use natural language**
```bash
# ✅ Good
hydra-llm-tldr search "implements circuit breaker pattern for external API calls"

# ❌ Bad - keyword soup
hydra-llm-tldr search "circuit breaker api external"
```

## Categories for cass_memory

| Category | Use For |
|----------|---------|
| security | Authentication, authorization, encryption |
| testing | Test patterns, mocking, assertions |
| debugging | Debugging techniques, common bugs |
| performance | Optimization, caching, concurrency |
| architecture | Design patterns, structure |
| database | Schema design, queries, migrations |

## Anti-Patterns (Don't Do This)

❌ **Skipping research** - "I'll just wing it"
→ Result: Reinventing wheels, repeating mistakes

❌ **Searching by filename** - `hydra-llm-tldr search "user.go"`
→ Result: Misses semantic matches, finds wrong files

❌ **No impact analysis** - Changing public APIs without checking
→ Result: Breaking changes, regressions

❌ **One-and-done queries** - Querying once, never updating
→ Result: Stale context, missing new patterns

❌ **Ignoring anti-patterns** - Not checking cass_memory warnings first
→ Result: Repeating known mistakes

## Success Indicators

**You're using skills correctly if:**
- ✅ Every implementation starts with `cass-memory context`
- ✅ New features include TLDR search
- ✅ Refactors include impact analysis
- ✅ You reference findings in your solution
- ✅ You're aware of anti-patterns before coding

**You're NOT using skills if:**
- ❌ Jumping straight to implementation
- ❌ Discovering patterns after coding
- ❌ Breaking things impact analysis would catch
- ❌ Repeating mistakes cass_memory warned about

## Related Skills

- **pattern-quality-reviewer** - Review and save patterns to cass_memory after completion
- **lsp-code-analysis** - Navigate code with LSP (go-to-definition, find references)
- **systematic-debugging** - Structured debugging methodology

## Quick Reference

```bash
# Always run first
cass-memory context "<your task>" --json

# Find code by behavior
hydra-llm-tldr search "<what it does>"

# Check impact before changing
hydra-llm-tldr impact <file> --depth 2

# List patterns by category
cass-memory list --category <category> --json
```
