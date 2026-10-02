# PRD: Security and delivery audit batch

### FR-010: Audit auth token handling
**Type:** audit
**Priority:** P1

## Task

Review token issuance, storage, and revocation paths for security defects.
Produce findings as a gap report; do not fix defects in this task.

### FR-011: Batch token revocation endpoint
**Type:** fr
**Priority:** P2
**Target Files:** `server/tokens.go`

## Acceptance Criteria

- Revoking multiple tokens in one request works
