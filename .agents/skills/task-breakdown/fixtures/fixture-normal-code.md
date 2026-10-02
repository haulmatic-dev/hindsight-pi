### FR-030: Sort query results by priority
**Type:** fr
**Priority:** P3
**Target Files:** `store/query.go`, `store/query_test.go`

## Acceptance Criteria

- Query results are ordered by priority descending
- Ties break on ID ascending
- Existing query tests still pass
