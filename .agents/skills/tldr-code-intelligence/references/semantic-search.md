# Semantic Search Patterns

## Effective Search Queries

### DO: Describe Intent

```bash
# ✅ Good - describes what you're looking for
llm-tldr search "validates user credentials against database"

# ❌ Bad - just keywords
llm-tldr search "user validation database"
```

### DO: Use Natural Language

```bash
# ✅ Good
llm-tldr search "handles pagination for large result sets"

# ❌ Bad
llm-tldr search "pagination handler limit offset"
```

## Search Patterns by Use Case

### Finding Implementations

```bash
# Look for specific patterns
llm-tldr search "singleton pattern implementation"

# Find middleware
llm-tldr search "middleware that logs request duration"

# Find error handling
llm-tldr search "centralized error recovery"
```

### Understanding Code Organization

```bash
# Find service layers
llm-tldr search "business logic service layer"

# Find data access
llm-tldr search "database access layer repository"

# Find API handlers
llm-tldr search "HTTP request handler endpoints"
```

### Debugging Support

```bash
# Find error sources
llm-tldr search "returns authentication errors"

# Find timeout handling
llm-tldr search "handles request timeouts"

# Find logging
llm-tldr search "structured logging implementation"
```

## Advanced Search Techniques

### Chaining Searches

```bash
# Step 1: Broad search
llm-tldr search "authentication" --limit 3

# Step 2: Narrow down
llm-tldr search "JWT token validation" --limit 5

# Step 3: Specific implementation
llm-tldr search "refresh token rotation"
```

### Combining with Context

```bash
# Search then get details
llm-tldr search "rate limiting" --limit 1
# Note the file from results

llm-tldr context internal/middleware/ratelimit.go
```
