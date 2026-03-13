---
name: intelligence-usage-guide
description: When and how to use cass_memory and TLDR based on task type. This skill guides Pi agents on when to query institutional knowledge vs when to proceed directly.
---

# Intelligence Usage Guide

## When to Query cass_memory

### ALWAYS Query (Before ANY Implementation)
```bash
cm context "<your exact task description>" --json
```

**Examples:**
- Adding new features → `cm context "implement user authentication" --json`
- Fixing bugs → `cm context "fix memory leak in worker pool" --json`
- Refactoring → `cm context "refactor database layer" --json`
- Writing tests → `cm context "unit testing async functions" --json`

### What to Look For:
1. **Patterns** - Successful approaches from past similar tasks
2. **Anti-patterns** - Mistakes to avoid (check these FIRST)
3. **Category-specific** insights based on your agent type

### Decision Matrix:

| Task Type | Query cass_memory? | Priority |
|-----------|-------------------|----------|
| New feature | ✅ Yes | High |
| Bug fix | ✅ Yes | High |
| Refactoring | ✅ Yes | Critical |
| Documentation | ✅ Yes | Medium |
| Code review | ✅ Yes | High |
| Simple typo fix | ❌ No | Low |
| Rename variable | ❌ No | Low |

---

## When to Search with TLDR

### Scenario 1: NEW Feature Implementation
**Question:** "Does similar functionality already exist?"

```bash
# Search for related implementations
llm-tldr search "<feature concept>"

# Example: Adding rate limiting
llm-tldr search "rate limiting middleware"
```

**Use When:**
- ✅ Adding NEW functionality
- ✅ Unclear where to add code
- ✅ Need to understand existing patterns
- ✅ Looking for examples to follow

**Don't Use When:**
- ❌ File path already specified in task
- ❌ Modifying specific known file
- ❌ Simple edits to existing code

---

### Scenario 2: Bug Investigation
**Question:** "Where is this functionality implemented?"

```bash
# Search by behavior, not file name
llm-tldr search "<what the code does>"

# Example: Finding auth logic
llm-tldr search "validates JWT tokens"
```

**Use When:**
- ✅ Debugging issues
- ✅ Finding source of errors
- ✅ Understanding unfamiliar code
- ✅ Tracing data flow

---

### Scenario 3: Pre-Refactoring Impact Analysis
**Question:** "What will break if I change this?"

```bash
# Step 1: Find the file (if not specified)
llm-tldr search "<component/functionality>"

# Step 2: Analyze impact
llm-tldr impact <found-file> --depth 2
```

**CRITICAL:** Always run impact analysis before:
- Modifying public APIs
- Changing interfaces
- Renaming functions
- Moving files

---

### Scenario 4: Understanding Dependencies
**Question:** "What does this code depend on?"

```bash
llm-tldr impact <file> --depth 1
```

**Use When:**
- ✅ Understanding codebase structure
- ✅ Planning architectural changes
- ✅ Identifying coupling
- ✅ Preparing for extraction/moving

---

## Quick Decision Tree

```
Starting new task
    ↓
Is it a new feature?
    ↓ YES
Query cass_memory → Search TLDR → Implement
    ↓ NO
Is it modifying existing code?
    ↓ YES
Is the file specified?
    ↓ YES
Query cass_memory → Check impact → Modify
    ↓ NO (file not specified)
Search TLDR → Find file → Check impact → Modify
    ↓ NO (not modifying)
Is it debugging?
    ↓ YES
Search TLDR → Fix
    ↓ NO
Query cass_memory → Proceed
```

---

## Workflow Examples

### Example 1: Add New API Endpoint

```bash
# Step 1: Check institutional knowledge
cm context "REST API endpoint implementation" --json

# Step 2: Find existing patterns
llm-tldr search "API endpoint handler implementation"

# Step 3: Look at best example found
# Step 4: Implement following pattern
```

### Example 2: Fix Authentication Bug

```bash
# Step 1: Check for known auth issues
cm context "authentication bug" --category security --json

# Step 2: Find auth implementation
llm-tldr search "user authentication logic"

# Step 3: Check impact of auth module
llm-tldr impact internal/auth/handler.go --depth 2

# Step 4: Fix with full context
```

### Example 3: Refactor Database Layer

```bash
# Step 1: Check for refactoring patterns
cm context "database layer refactoring" --category architecture --json

# Step 2: Find database implementations
llm-tldr search "database access layer repository"

# Step 3: Check impact of each file
llm-tldr impact internal/db/connection.go --depth 2
llm-tldr impact internal/db/repository.go --depth 2

# Step 4: Plan migration
# Step 5: Execute carefully
```

---

## Anti-Patterns (Don't Do This)

❌ **Skipping research** - "I'll just wing it"
→ Result: Reinventing wheels, repeating mistakes

❌ **Searching by filename** - `llm-tldr search "user.go"`
→ Result: Misses semantic matches, finds wrong files

❌ **No impact analysis** - Changing public APIs without checking
→ Result: Breaking changes, regressions

❌ **One-and-done queries** - Querying once, never updating
→ Result: Stale context, missing new patterns

---

## Success Indicators

**You're using skills correctly if:**
- ✅ Every implementation starts with `cm context`
- ✅ New features include TLDR search
- ✅ Refactors include impact analysis
- ✅ You reference findings in your solution
- ✅ You're aware of anti-patterns before coding

**You're NOT using skills if:**
- ❌ Jumping straight to implementation
- ❌ Discovering patterns after coding
- ❌ Breaking things that impact analysis would catch
- ❌ Repeating mistakes cass_memory warned about
