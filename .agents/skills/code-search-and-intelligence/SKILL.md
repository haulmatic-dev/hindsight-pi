---
name: code-search-and-intelligence
description: Query institutional knowledge with Hindsight and search code semantically with hydra-llm-tldr. This skill provides actionable commands that MUST be executed before implementing any code. Run these commands first to discover patterns, avoid anti-patterns, and understand the codebase.
---

# Code Search and Intelligence

This skill provides **two intelligence commands** that you MUST run before implementing:

1. **hindsight** → query institutional knowledge
2. **hydra-llm-tldr** → search code semantically

## ⚠️  CRITICAL: Run These Commands FIRST

**Before writing any code, you MUST execute these commands in order:**

### Step 1: Query Institutional Knowledge (MANDATORY)

```bash
hindsight search "<your task description>" --limit 5
```

**Run this FIRST for every task.** This retrieves:
- Proven patterns from past work
- Anti-patterns to avoid (check these FIRST)
- Architectural guidance

**Example:**
```bash
hindsight search "implement user authentication with JWT tokens" --limit 5
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

### Good Hindsight Queries
```bash
# ✅ Be specific about intent
hindsight search "implement OAuth2 with refresh tokens" --limit 5

# ✅ Include the problem context
hindsight search "debug memory leak in worker pool" --limit 5
```

### Good TLDR Search Queries
```bash
# ✅ Describe behavior, not keywords
hydra-llm-tldr search "validates JWT tokens"

# ✅ Be specific but not too narrow
hydra-llm-tldr search "database transaction rollback handling"
```

## Available Commands Reference

### hindsight - Institutional Knowledge

Query patterns and anti-patterns from past work.

```bash
# Query context for your task (ALWAYS RUN THIS FIRST)
hindsight search "<task description>" --limit 5

# Search with minimum relevance threshold
hindsight search "<task>" --min-relevance 0.7 --limit 5

# Get context for a specific bead
hindsight context --bead-id <bead-id> --limit 5
```

**Relevance Scores:**
- 0.8+ - Highly relevant (use immediately)
- 0.6-0.8 - Moderately relevant (review carefully)
- 0.4-0.6 - Weakly relevant (may not apply)
- <0.4 - Not relevant (ignore)

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
hindsight search "implement user authentication with JWT" --limit 5

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

- **pattern-quality-reviewer** - Review patterns before saving to Hindsight
- **lsp-code-analysis** - Navigate code with LSP
- **systematic-debugging** - Structured debugging methodology

## Quick Reference

```bash
# ALWAYS run first
hindsight search "<your task>" --limit 5

# For implementation tasks
hydra-llm-tldr search "<what to implement>"

# For modifications
hydra-llm-tldr impact <file> --depth 2
```
