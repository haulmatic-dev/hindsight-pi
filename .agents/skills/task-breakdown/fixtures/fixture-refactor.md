### FR-002: Reduce duplication in the notifier layer
**Type:** refactor
**Priority:** P3
**Target Files:** `notify/notifier.go`

## Instructions

Simplify and refactor code while preserving behavior: extract shared helpers,
remove duplication, improve naming. All existing tests must keep passing
before and after the change.

## Acceptance Criteria

- No behavior change; existing tests pass
- Duplication between the email and SMS paths is removed
