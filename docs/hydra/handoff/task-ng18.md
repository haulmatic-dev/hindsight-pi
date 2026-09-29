# task handoff

## What changed (files + one line each)
- docs/hydra/handoff/task-ng18.md — this document (human-review integration triage of hydra-242b; no code changes).

## Verified findings (hydra-242b integration state)
- Branch `hydra/wt-hydra-242b` (tip 9945368) unique content commit `c3f3500 feat: auto-expire stale human escalations`.
- That feature ALREADY landed on origin/development as `2ee7449 feat: escalation TTL` — `internal/core/escalation_ttl.go` present, `overseer.escalation_ttl` default 72h.
- `git merge-tree --write-tree origin/development hydra/wt-hydra-242b` → exit 1 (conflicts: docs/hydra/handoff/task-242b.md add/add, internal/core/config.go).
- `git diff origin/development hydra/wt-hydra-242b --stat` = 64 files, +272/−3647 → branch deletes internal/triage/*, internal/degrade/*, pan degrade hooks (all newer landed work). Rebase/cherry-pick REJECTED — would revert newer code.
- Landed triage `docs/discovery/integration-triage-242b.md` on origin/development prescribes: abandon-with-summary, do NOT rebase, do NOT clear-hard-park.

## Actions taken (live store, /opt/dev-tools/components/hydra)
- `br doctor migrate-schema recover` — cleared "database is busy (recovery in progress)" WAL poison (twice; second attempt hit private-recovery retry, resolved).
- Triage comment added to hydra-242b documenting supersession.
- `br close hydra-242b -r "not_planned: superseded by landed escalation_ttl (2ee7449)..."` — bead now CLOSED with reason "Superseded: escalation_ttl scanner (2ee7449) already shipped this behavior on development", label `triage-superseded` present; durable integration-exhausted label intentionally retained (park resolves via closure, per d778/014y precedent).

## Verification (commands run + pass/fail)
- `br show hydra-242b` — status CLOSED, closed reason + labels verified. PASS.
- `git show origin/development:internal/core/escalation_ttl.go` — present. PASS.
- `git merge-tree` / `git diff --stat` — conflict + stale-iteration evidence recorded above. PASS (evidence, not failure).

## Open edges for the next phase
- None for this bead. Recurring pattern: every inspected integration-exhausted branch (d778, 014y, 242b) was a superseded iteration; consider Pan recipe that closes such beads directly once triage doc exists.
- `.beads` WAL "database is busy" keeps recurring on this store family; investigate root lock holder (daemon concurrent pan scans) rather than repeated `br doctor` recover.
