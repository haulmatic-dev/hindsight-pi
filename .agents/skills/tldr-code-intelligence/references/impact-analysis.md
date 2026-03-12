# Impact Analysis Guide

## Understanding Impact Results

### The Impact Object

```json
{
  "file": "target/file.go",
  "impact": {
    "callers": [...],  // Who calls this
    "callees": [...]   // What this calls
  },
  "scope": {
    "files": 15,       // Total files affected
    "tokens": 12500,   // Approximate code size
    "exceeded": false  // Did we hit limits?
  }
}
```

## When to Use Impact Analysis

### Before Refactoring

```bash
# Check impact of changing an interface
llm-tldr impact internal/interfaces/store.go --depth 2

# Check impact of moving a function
llm-tldr impact internal/utils/helpers.go --depth 2

# Check impact of renaming
llm-tldr impact internal/models/user.go --depth 2
```

### Before Deleting Code

```bash
# Check if anything uses this
llm-tldr impact internal/old/legacy.go --depth 3
```

### Before Changing APIs

```bash
# Check public API impact
llm-tldr impact pkg/api/client.go --depth 2
```

## Interpreting Scope

### Small Scope (1-5 files)
- Safe to refactor
- Low risk
- Quick to validate

### Medium Scope (5-20 files)
- Requires careful planning
- Consider staged changes
- Write tests first

### Large Scope (20+ files)
- Major undertaking
- Requires architectural review
- Consider deprecation strategy

### Exceeded = true
- Analysis hit token limits
- Incomplete picture
- Consider breaking into smaller pieces

## Common Scenarios

### Scenario 1: Adding a Parameter

```bash
# Check what calls this function
llm-tldr impact internal/service/order.go --depth 2

# Review callers list
# Plan signature change
# Update all call sites
```

### Scenario 2: Moving Code

```bash
# Check current dependencies
llm-tldr impact internal/utils/common.go --depth 2

# Check destination package
llm-tldr impact internal/helpers/common.go --depth 1

# Plan import updates
```

### Scenario 3: Interface Changes

```bash
# Find all implementations
llm-tldr impact internal/interfaces/database.go --depth 3

# Check each implementation
# Plan migration strategy
```
