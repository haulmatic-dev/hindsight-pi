# discovery handoff

## What changed (files + one line each)
- docs/hydra/handoff/task-242b.md: this handoff (triage-only bead; no code change required or made).

## Verified findings (hydra-242b integration state)

Work status: SUPERSEDED, integration not needed.
- Branch `hydra/wt-hydra-242b` holds 2 commits (pushed: origin/dogfood/hydra-242b == 9945368, matches worktree branch tip):
  - c3f3500 feat: auto-expire stale human escalations (overseer.escalation_expiry_hours, default 48h)
  - 9945368 docs: green handoff for hydra-242b
- Feature files in 242b: `internal/core/escalation_expiry.go` + `_test.go`, config knob `overseer.escalation_expiry_hours` (48h), event `bead.escalation_expired`.
- SUPERSEDED by landed commit `2ee7449` on origin/development: `feat: escalation TTL — age stale human escalations back to ready queue` — `internal/core/escalation_ttl.go` implements the identical mechanism (`escalated` label demoted to `escalation-expired` past TTL, bead returned to ready queue, typed `escalation_expired` event, knob `overseer.escalation_ttl` default 72h). 242b predates/copies the landed implementation (worker-author dates both 2026-09-29); rebasing would add a duplicate scanner.
- Merge-tree vs origin/development: single content conflict in `internal/core/config.go` (both sides added their respective TTL knob fields to the same struct region). CHANGELOG.md, docs/operations/host-setup.md, hydra-config.yaml.example, internal/core/overseer.go auto-merge.
- Park root cause: the same shared integration wedge documented in docs/discovery/integration-triage-s402-shared-wedge.md (origin/development) and the d778/014y triage — repeated push/PR failures on the working branch, not a defect in 242b's change.

## Disposition (for the human review this bead requests)
- Do NOT rebase or merge 242b — its feature already landed as escalation_ttl.
- Consciously ABORT the integration: clear the `integration-exhausted` park on hydra-242b and close/abandon the bead with a superseded-by-2ee7449 summary. `repair:clear-hard-park` (Pan hygiene verb) is appropriate now because the "cause" (missing escalation-expiry mechanism) is resolved by the landed feature.
- Recurrence path already exists: Pan `file-bead` (MaxOpenFiledBeadsPerProject=2, pan-filed label) auto-files review requests after repeated failures; no new hydra-level escalation path needed.

## Open edges for the next phase
- Operator action only: clear park + abandon hydra-242b. No code work.
- If a reviewer instead prefers the 48h TTL over the landed 72h: config-only change on development (`overseer.escalation_ttl: 48h`), not a merge of 242b.
