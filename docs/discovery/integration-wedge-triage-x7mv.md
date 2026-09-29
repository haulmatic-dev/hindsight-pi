# Shared wedge: integration-exhausted hard-parks clustering across beads (hydra-x7mv)

Date: 2026-09-29 · Author: Hydra worker (bead hydra-x7mv)

## Affected beads

- hydra-xxq5 — "18 beads hard-parked with integration-exhausted within minutes" (hydra store)
- hydra-uqxe — "integration-exhausted hard-park storm" (hydra store)
- hydra-242b — escalated-label wedge bead, also carrying `integration-exhausted` (hydra store)
- ajantha-srv-telematics-otae — "5 beads hard-parked integration-exhausted" (srv-telematics store)

## Root cause

Not integration capacity, not base-branch churn, not a worktree/queue defect. The
mass simultaneous hard-parks are the documented poisoned-br-WAL-index signature:

- The srv-telematics store carried 170+ stale `.br-wal-index-*` litter
  directories (215 remaining after two doctor passes, all pre-recovery family).
- The hydra store carried 64 such directories.
- The known transient failure mode of this state is
  `Database error: database is busy (recovery in progress)` — br writes fail,
  integration/publish bookkeeping errors out, beads exhaust their retries
  within the same minutes-long window and hard-park together.

This matches the codebase playbook proven rule: "Mass integration-exhausted
hard-parks within minutes = poisoned br WAL index (`database is busy (recovery
in progress)`), not integration capacity; repair is `br doctor migrate-schema
recover`, often run twice (first pass re-poisoned by contending writer)."

## Repair applied (verified)

- `br doctor migrate-schema recover` ×2 on /opt/dev-tools/components/hydra/.beads
  → "Recovered engine read admission for .../beads.db" both passes.
- Same ×2 on /home/ajantha/projects/srv-telematics/.beads → recovered both passes.
- Post-repair reads verified clean (`br show` / `br list` / `br close` all
  succeed with no "database is busy" errors).

## Disposition

- hydra-xxq5, hydra-uqxe, hydra-242b: CLOSED (not_planned) as duplicate-class
  pan-filed wedge beads; root cause fixed. hydra-242b additionally had
  `integration-exhausted` / `integration-failed` cleared and holds a triage
  comment (its escalation sub-issue was already superseded by the landed
  escalation_ttl scanner, 2ee7449).
- ajantha-srv-telematics-otae: `integration-exhausted` / `integration-failed`
  cleared for one fair scheduler retry (cause fixed), triage comment recorded.
- Human action originally requested in the task is satisfied: the root cause
  turned out to be repairable via the documented br doctor path, so no
  un-automatable merge/PR fix was required.

## Open edges

- WAL litter accumulates again under daemon concurrency; consider a periodic
  store-health check that flags fresh `.br-wal-index-*` growth and auto-runs
  `br doctor migrate-schema recover` fail-open.
- If any of the cleared beads re-park with the same signature within hours,
  the WAL repair did not hold — re-run doctor twice and investigate the
  contending writer instead of per-bead hygiene.
