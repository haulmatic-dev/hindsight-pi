# Dedup triage: hydra-uewg — shared integration-exhausted wedge

## Disposition
hydra-uewg is a duplicate of the pan-filed shared-wedge bead **hydra-qzxv**
(identical title and proposal, both filed 2026-09-29). Closed
`not_planned: duplicate`, `pan-filed` label removed.

## Dedup evidence
- `br show hydra-uewg` (hydra store): open, pan-filed, title "Shared wedge:
  mass integration-exhausted hard-parks across ajantha/srv-telematics…".
- `br show hydra-qzxv`: open, pan-filed, same title, carries the same
  proposal (human triage of integration pipeline, batch-clear parks after
  fix, fleet-wide circuit-breaker idea).
- srv-telematics store has its own canonical shared-wedge bead
  `ajantha-srv-telematics-xlkw` (open, pan-filed, same title).

## Code-change proposal status (already satisfied)
The uewg proposal "integration retry / reuse of existing PRs" is already
landed on development:
- `internal/core/publisher.go`: `isPRAlreadyExists` classifier +
  `resolveOrCreatePR` — `gh pr create` "already exists" resolves and merges
  the pre-existing PR instead of failing (prior handoff task-bwyn).
- `internal/core/publisher.go`: `publishWithRebaseRetry` — conflict → rebase
  → retry, cap 2 (FR-011).
- `internal/core/integration_retest.go`: time-boxed retest of parked
  integrations (max_attempts, retry_interval, enabled in live config).
- `internal/core/integration_gate.go`: per-repo gated serialization +
  30s cooldown.

Remaining human action (tracked on hydra-qzxv): investigate PR #389
publisher wedge, then batch-clear integration-exhausted parks via the
operator unpark endpoint.
