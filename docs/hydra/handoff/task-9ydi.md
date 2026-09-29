# discovery handoff

## What changed (files + one line each)
- docs/hydra/handoff/task-9ydi.md: this handoff (triage-only bead; no code change).
- docs/hydra/handoff/task-242b.md: full triage findings for hydra-242b.
- CHANGELOG.md: Documentation entry recording the disposition.

## Verification (commands run + pass/fail)
- `git merge-tree --write-tree origin/development hydra/wt-hydra-242b` — exit 1, CONFLICT in internal/core/config.go (both sides add an escalation-expiry config field).
- `git grep -l EscalationTTL origin/development` — hits config.go, escalation_ttl.go, escalation_ttl_test.go, overseer.go → EscalationTTLScanner (`overseer.escalation_ttl`, default 72h) already landed on development, superseding 242b's `escalation_expiry_hours` (48h, commit c3f3500). Pan `clear-escalation` repair also landed.
- `git show c3f3500 --stat` — confirms 242b's change is the older iteration of the same feature.
- Gate log /opt/dev-tools/var/hydra/gates/hydra/hydra-242b/test.log — all packages ok, no FAIL line; park cause is the config.go conflict + failed PR re-creation, not a gate failure.
- Commit b453bb9 on this branch contains both handoffs + changelog entry.

## Open edges for the next phase
- Disposition (human-triage answer): CLOSE/abandon hydra-242b as superseded. Do NOT clear the hard-park (re-merge would duplicate the landed escalation_ttl feature and conflict in config.go); do NOT rebase.
- The escalation wedge the pan-filed bead tracked is already fixed on development by the TTL scanner (72h) — stale escalations clear on the next sweep.
- No Pan verb closes a bead as superseded; closure is a human/Pan `abandon-with-summary` action.
