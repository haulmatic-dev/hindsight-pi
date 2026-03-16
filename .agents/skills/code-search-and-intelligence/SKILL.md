---
name: code-search-and-intelligence
description: Query institutional knowledge with cass-memory and search code semantically with hydra-llm-tldr. This skill provides actionable commands that MUST be executed before implementing any code. Run these commands first to discover patterns, avoid anti-patterns, and understand the codebase.
---

# Code Search and Intelligence

This skill provides **two intelligence commands** that you MUST run before implementing:

1. **cass-memory** → query institutional knowledge
2. **hydra-llm-tldr** → search code semantically

## ⚠️  CRITICAL: Run These Commands FIRST

**Before writing any code, you MUST execute these commands in order:**

### Step 1: Query Institutional Knowledge (MANDATORY)

```bash
cass-memory context "<your task description>" --json
```

**Run this FIRST for every task.** This retrieves:
- Proven patterns from past work
- Anti-patterns to avoid (check these FIRST)
- Architectural guidance

**Example:**
```bash
cass-memory context "implement user authentication with JWT tokens" --json
```

### Step 2: Search Code Semantically (MANDATORY for implementation)

```bash
hydra-llm-tldr search "<what you need to implement>"
```

**Run this SECOND to find existing code before writing new code.**

**Example:**
```bash
hydra-llm-tldr search "authentication middleware implementation"
```

### Step 3: Analyze Impact (MANDATORY for modifications)

If modifying existing code:

```bash
hydra-llm-tldr impact <file-path> --depth 2
```

**Example:**
```bash
hydra-llm-tldr impact internal/auth/handler.go --depth 2
```

## Why This Matters

✅ **Patterns** - Find proven solutions from past work  
✅ **Anti-patterns** - Avoid known mistakes  
✅ **Context** - Understand the codebase before changing it  
✅ **Impact** - Know what depends on code before modifying it  

## Query Best Practices

### Good cass-memory Queries
```bash
# ✅ Be specific about intent
cass-memory context "implement OAuth2 with refresh tokens" --json

# ✅ Include the problem context
cass-memory context "debug memory leak in worker pool" --json
```

### Good TLDR Search Queries
```bash
# ✅ Describe behavior, not keywords
hydra-llm-tldr search "validates JWT tokens"

# ✅ Be specific but not too narrow
hydra-llm-tldr search "database transaction rollback handling"
```

## Available Commands Reference

### cass-memory - Institutional Knowledge

Query patterns and anti-patterns from past work.

```bash
# Query context for your task (ALWAYS RUN THIS FIRST)
cass-memory context "<task description>" --json

# Query with specific category
cass-memory context "authentication" --category security --json

# List available patterns by category
cass-memory list --category <category> --json
```

**Categories:** security, testing, debugging, performance, architecture, database

### hydra-llm-tldr - Semantic Code Search

Find code by what it does, not what it's named.

```bash
# Search by behavior description
hydra-llm-tldr search "<behavior description>"

# Search with limit
hydra-llm-tldr search "<description>" --limit 5

# Search in specific directory
hydra-llm-tldr search "<description>" --path <directory>

# Analyze impact before changing
hydra-llm-tldr impact <file> --depth 2
```

## Complete Workflow Example

**Scenario: Implement user authentication**

```bash
# 1. Query institutional knowledge FIRST
cass-memory context "implement user authentication with JWT" --json

# 2. Search for existing authentication code
hydra-llm-tldr search "JWT authentication middleware"

# 3. If modifying existing auth code, check impact
hydra-llm-tldr impact internal/auth/middleware.js --depth 2

# 4. Now implement with full context
```

## Common Mistakes to Avoid

❌ **Skipping research** - Don't implement without querying first  
❌ **Searching by filename** - Use behavior descriptions instead  
❌ **No impact analysis** - Always check dependencies before modifying  
❌ **Ignoring results** - Use findings to guide your implementation  

## Related Skills

- **pattern-quality-reviewer** - Save patterns to cass_memory after completion
- **lsp-code-analysis** - Navigate code with LSP
- **systematic-debugging** - Structured debugging methodology

## Quick Reference

```bash
# ALWAYS run first
cass-memory context "<your task>" --json

# For implementation tasks
hydra-llm-tldr search "<what to implement>"

# For modifications
hydra-llm-tldr impact <file> --depth 2
```
