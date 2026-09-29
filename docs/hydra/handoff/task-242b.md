# discovery handoff (hydra-242b)

## What changed (files + one line each)
- docs/hydra/handoff/task-242b.md: this handoff (triage-only; no code change).
- CHANGELOG.md: Documentation entry pointing at the disposition.

## Verified findings (hydra-242b integration state)

Bead identity: pan-filed PROJECT-WEDGE triage bead "3 beads stuck >24h on stale human escalations" (labels: integration-exhausted, integration-failed, pan-filed).

Branch state: `hydra/wt-hydra-242b` holds 2 commits (c3f3500 feat: auto-expire stale human escalations `escalation_expiry_hours` 48h; 9945368 docs handoff), pushed to origin/dogfood/hydra-242b.

Cause of integration-exhausted: branch conflicts with origin/development in `internal/core/config.go` (merge-tree exit 1); PR re-creation fails (PR for dogfood/hydra-242b already exists pattern).

Key finding — SUPERSEDED: origin/development already contains the same fix, newer iteration:
- `internal/core/escalation_ttl.go`: EscalationTTLScanner, `overseer.escalation_ttl` default 72h (vs this branch's `escalation_expiry_hours` 48h), event `escalation_expired`, demotes stale `escalated` labels.
- Pan `clear-escalation` repair also landed (one fair retry after cause fixed).
Both cover this branch's problem domain; merging/rebasing 242b would duplicate the feature and conflict in config.go.

Gate evidence: /opt/dev-tools/var/hydra/gates/hydra/hydra-242b/test.log — all packages ok, no failing package.

## Disposition (human-triage answer)
Do NOT clear the park; do NOT rebase. Close/abandon hydra-242b with summary "superseded by escalation_ttl scanner + clear-escalation repair already on development". The underlying escalation wedge the bead tracked is fixed by landed code; the stale escalations it references are handled by the TTL scanner (72h) on the next sweep.

## Open edges for the next phase
- Pan repair verbs: no verb closes a bead as superseded — human (or abandon-with-summary judgment) must close hydra-242b.
- Consolidation: two TTL knobs exist conceptually (48h proposed vs 72h landed); landed 72h is authoritative.
