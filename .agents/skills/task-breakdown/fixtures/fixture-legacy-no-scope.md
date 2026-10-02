### FR-020: Backfill audit trail for legacy ingest workers
**Type:** fr
**Priority:** P3

## Task

The legacy ingest workers in the old service predate the current task
template and have no explicit target files listed. Record how their audit
trail is written so the behavior is not lost when the legacy service is
decommissioned.

## Acceptance Criteria

- The audit trail write path is documented
