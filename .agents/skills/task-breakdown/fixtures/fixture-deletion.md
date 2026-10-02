### FR-003: Remove deprecated v1 webhook endpoints
**Type:** deletion
**Priority:** P2
**Target Files:** `server/webhook_v1.go`, `server/routes.go`

## Task

Delete the deprecated v1 webhook endpoints and any dead code that only they
reference. Registration entries in routes.go and the v1 handler file are
removed entirely.

## Acceptance Criteria

- The v1 handler file is deleted
- Routes no longer register any v1 endpoints
- Build and tests pass with no references to the removed code
