# task handoff

## What changed (files + one line each)
- docs/hydra/handoff/task-ng18.md — this handoff (integration triage for hydra-242b; no code change — resolution is in the live bead store, not the repo).

## Triage findings (hydra-242b, integration-exhausted)
- Bead = "PROJECT-WEDGE: stale human escalations" pan-filed task; branch `hydra/wt-hydra-242b` (2 commits, incl. c3f3500 auto-expire stale escalations via `overseer.escalation_expiry_hours=48h`).
- Merge-tree vs origin/development: conflicts in docs/hydra/handoff/task-242b.md and internal/core/config.go.
- SUPERSEDED: origin/development already ships `internal/core/escalation_ttl.go` from commit 2ee7449 (`overseer.escalation_ttl`, default 72h) — same problem domain, richer semantics (label swap to `escalation-expired` + typed `escalation_expired` event). Prior triage commits a9f9e15 and b453bb9 on development reach the same disposition.
- Store was WAL-poisoned ("database is busy (recovery in progress)"); cleared via `br doctor migrate-schema recover`.

## Actions taken (live store, /opt/dev-tools/components/hydra)
- `br doctor migrate-schema recover` — PASS (cleared WAL-index poison).
- `br update hydra-242b --add-label triage-superseded` — ✓.
- `br comment add hydra-242b <triage summary>` — ✓.
- `br close hydra-242b --reason "Superseded: escalation_ttl scanner (2ee7449) already shipped..."` — ✓ closed.

## Disposition rationale
Integration resolution here is NOT a merge: rebasing would duplicate shipped functionality and revert newer escalation semantics. Per fleet policy (superseded-iteration precedent from d778/014y triage), correct resolution is close-with-summary; hard-park label becomes moot on a closed bead. Branch retained for audit.

## Open edges for the next phase
- None for 242b. Recurring WAL-poison on the shared beads store keeps resurfacing across beads — consider a periodic `br doctor` sweep or mode revert.
