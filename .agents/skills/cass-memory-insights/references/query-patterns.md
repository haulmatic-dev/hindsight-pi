# Query Pattern Guide

## Effective Query Construction

### DO: Be Specific About Intent

Good queries include what you're trying to accomplish:

```bash
# ✅ Good - clear intent
cm context "implement rate limiting for API endpoints" --json

# ❌ Bad - too vague
cm context "rate limiting" --json

# ❌ Bad - too specific with implementation details
cm context "implement token bucket rate limiting with Redis backend" --json
```

### DO: Use Natural Language

CASS understands natural language better than keywords:

```bash
# ✅ Good
cm context "debug memory leaks in long-running processes" --json

# ❌ Bad - keyword soup
cm context "memory leak goroutine debug" --json
```

### DO: Include Context

Add relevant context to get better matches:

```bash
# ✅ Good - includes technology context
cm context "database migration rollback strategy PostgreSQL" --json

# ❌ Bad - missing context
cm context "database rollback" --json
```

## Category-Specific Queries

### Security

```bash
# Authentication patterns
cm context "OAuth2 authentication flow implementation" --category security --json

# Authorization
cm context "role-based access control patterns" --category security --json

# Common vulnerabilities
cm context "preventing SQL injection attacks" --category security --json
```

### Testing

```bash
# Test patterns
cm context "table-driven tests in Go" --category testing --json

# Mocking strategies
cm context "mocking external API calls in tests" --category testing --json

# Test organization
cm context "organizing test suites by functionality" --category testing --json
```

### Performance

```bash
# Caching
cm context "caching strategies for frequently accessed data" --category performance --json

# Database optimization
cm context "database query optimization techniques" --category performance --json

# Concurrency
cm context "managing concurrent request handling" --category performance --json
```

### Debugging

```bash
# Error investigation
cm context "tracing errors through call stack" --category debugging --json

# Race conditions
cm context "detecting and fixing race conditions" --category debugging --json

# Logging
cm context "structured logging for debugging" --category debugging --json
```

## Interpreting Results

### High Scores (0.8+)

Patterns with high scores are highly relevant to your query. Prioritize these.

### Medium Scores (0.5-0.8)

These may provide useful context or alternative approaches.

### Anti-Patterns

Always check anti-patterns first. Avoiding mistakes is as important as following patterns.

### History Snippets

These provide context from previous implementations but may be less structured than bullets.

## Common Mistakes

### Mistake 1: Not Querying Before Starting

❌ **Wrong:** Start coding, hit a problem, then query CASS
✅ **Right:** Query CASS first to learn from past mistakes

### Mistake 2: Ignoring Anti-Patterns

❌ **Wrong:** Only read relevant bullets, skip anti-patterns
✅ **Right:** Always check anti-patterns first

### Mistake 3: Queries Too Broad

❌ **Wrong:** `cm context "code" --json`
✅ **Right:** `cm context "error handling patterns in REST APIs" --json`

### Mistake 4: Not Using Categories

❌ **Wrong:** Generic query without category filter
✅ **Right:** Use `--category` to narrow results

## Advanced Usage

### Chaining Queries

```bash
# First, get general context
cm context "microservice communication" --json

# Then, drill into specific area
cm context "circuit breaker pattern implementation" --category architecture --json

# Finally, check for pitfalls
cm context "circuit breaker common mistakes" --category anti-pattern --json
```

### Combining with Other Skills

```bash
# 1. Query CASS for patterns
cm context "rate limiting" --json

# 2. Use lsp-code-analysis to find existing implementations
lsp search "RateLimiter" --kinds class

# 3. Check impact of changes
llm-tldr impact internal/api/middleware.go --depth 2
```
