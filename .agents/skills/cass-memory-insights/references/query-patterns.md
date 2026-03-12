# Query Pattern Guide

## Effective Query Construction

### DO: Be Specific About Intent

Good queries include what you're trying to accomplish:

```bash
# ✅ Good - clear intent
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "implement rate limiting for API endpoints" --json

# ❌ Bad - too vague
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "rate limiting" --json

# ❌ Bad - too specific with implementation details
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "implement token bucket rate limiting with Redis backend" --json
```

### DO: Use Natural Language

CASS understands natural language better than keywords:

```bash
# ✅ Good
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "debug memory leaks in long-running processes" --json

# ❌ Bad - keyword soup
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "memory leak goroutine debug" --json
```

### DO: Include Context

Add relevant context to get better matches:

```bash
# ✅ Good - includes technology context
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "database migration rollback strategy PostgreSQL" --json

# ❌ Bad - missing context
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "database rollback" --json
```

## Category-Specific Queries

### Security

```bash
# Authentication patterns
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "OAuth2 authentication flow implementation" --category security --json

# Authorization
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "role-based access control patterns" --category security --json

# Common vulnerabilities
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "preventing SQL injection attacks" --category security --json
```

### Testing

```bash
# Test patterns
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "table-driven tests in Go" --category testing --json

# Mocking strategies
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "mocking external API calls in tests" --category testing --json

# Test organization
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "organizing test suites by functionality" --category testing --json
```

### Performance

```bash
# Caching
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "caching strategies for frequently accessed data" --category performance --json

# Database optimization
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "database query optimization techniques" --category performance --json

# Concurrency
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "managing concurrent request handling" --category performance --json
```

### Debugging

```bash
# Error investigation
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "tracing errors through call stack" --category debugging --json

# Race conditions
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "detecting and fixing race conditions" --category debugging --json

# Logging
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "structured logging for debugging" --category debugging --json
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

❌ **Wrong:** `/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "code" --json`
✅ **Right:** `/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "error handling patterns in REST APIs" --json`

### Mistake 4: Not Using Categories

❌ **Wrong:** Generic query without category filter
✅ **Right:** Use `--category` to narrow results

## Advanced Usage

### Chaining Queries

```bash
# First, get general context
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "microservice communication" --json

# Then, drill into specific area
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "circuit breaker pattern implementation" --category architecture --json

# Finally, check for pitfalls
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "circuit breaker common mistakes" --category anti-pattern --json
```

### Combining with Other Skills

```bash
# 1. Query CASS for patterns
/Users/buddhi/source-code/personal/hydra/scripts/cm-wrapper.js context "rate limiting" --json

# 2. Use lsp-code-analysis to find existing implementations
lsp search "RateLimiter" --kinds class

# 3. Check impact of changes
llm-tldr impact internal/api/middleware.go --depth 2
```
