# Semantic Search Patterns

## Effective Search Queries

### DO: Describe Intent

```bash
# ✅ Good - describes what you're looking for
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "validates user credentials against database"

# ❌ Bad - just keywords
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "user validation database"
```

### DO: Use Natural Language

```bash
# ✅ Good
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "handles pagination for large result sets"

# ❌ Bad
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "pagination handler limit offset"
```

## Search Patterns by Use Case

### Finding Implementations

```bash
# Look for specific patterns
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "singleton pattern implementation"

# Find middleware
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "middleware that logs request duration"

# Find error handling
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "centralized error recovery"
```

### Understanding Code Organization

```bash
# Find service layers
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "business logic service layer"

# Find data access
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "database access layer repository"

# Find API handlers
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "HTTP request handler endpoints"
```

### Debugging Support

```bash
# Find error sources
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "returns authentication errors"

# Find timeout handling
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "handles request timeouts"

# Find logging
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "structured logging implementation"
```

## Advanced Search Techniques

### Chaining Searches

```bash
# Step 1: Broad search
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "authentication" --limit 3

# Step 2: Narrow down
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "JWT token validation" --limit 5

# Step 3: Specific implementation
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "refresh token rotation"
```

### Combining with Context

```bash
# Search then get details
/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js search "rate limiting" --limit 1
# Note the file from results

/Users/buddhi/source-code/personal/hydra/scripts/llm-tldr-wrapper.js context internal/middleware/ratelimit.go
```
