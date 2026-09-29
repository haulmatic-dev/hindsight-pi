# Discovery: hydra-242b (shared wedge — integration-exhausted hard-parks)

## Disposition

**abandon-with-summary. Do NOT rebase. Do NOT cherry-pick. Do NOT clear-hard-park** (matches the d778/014y disposition in `integration-triage-d778-014y.md`: close the bead with a summary; the park then resolves via closure, not label surgery).

hydra-242b's work is a SUPERSEDED iteration of already-landed work, same as
the d778/014y wedge documented in `integration-triage-d778-014y.md`.

## Verified evidence (this worktree, 2026-09-29+)

1. Branch `hydra/wt-hydra-242b` tip 9945368; unique content commit
   `c3f3500 feat: auto-expire stale human escalations (overseer.escalation_expiry_hours, default 48h)`.
2. That feature landed independently as `2ee7449 feat: escalation TTL — age
   stale human escalations back to ready queue` (`overseer.escalation_ttl`,
   default 72h, label → `escalation-expired`, scanner in
   internal/core/escalation_ttl.go). Functionally identical problem solved.
3. `git diff origin/development..hydra/wt-hydra-242b --stat` = **61 files,
   +272/−3497**, deleting internal/triage/* (FR-030), internal/degrade/*
   (FR-032), pan degrade hooks — all newer landed work. Rebasing or
   cherry-picking would revert these.
4. `git merge-tree --write-tree origin/development hydra/wt-hydra-242b`:
   conflicts only in `docs/hydra/handoff/task-242b.md` (add/add) and
   `internal/core/config.go` (TriageConfig/Config field churn). No
   substantive conflict.
5. Earlier discovery handoff already committed on this branch
   (a9f9e15): "superseded by landed escalation_ttl (2ee7449); disposition
   abandon-with-summary".

## Recommended operator actions

- Close `hydra-242b` as not-planned with a summary (Pan `abandon-with-summary` or manual close); per the d778/014y precedent, do NOT run `repair:clear-hard-park` — the durable label is accurate and closure is the correct exit.
- Re-attempting integration or cherry-picking c3f3500 is REJECTED: it would
  conflict with/replace the landed escalation_ttl scanner and delete newer
  code (see point 3).

## On the proposed hydra change (configurable integrate-stranded budget)

Partially already satisfied: integration-exhausted fires on
`consecutive_failures_capped` (internal/core/bead_closer.go, config.go,
integration_exhausted_gate_test.go). Any future change should reuse the
existing config plumbing; given every inspected stranded branch so far
(d778, 014y, 242b) turned out to be a superseded iteration, a larger retry
budget is unlikely to have helped — the durable park + human triage path
worked as designed. Recommendation: do NOT add a retry-budget knob now;
instead keep the existing `repair:clear-hard-park` + triage flow.
