# DISCOVERY handoff

## What changed (files + one line each)
- docs/discovery/integration-triage-242b.md — triage report: hydra-242b branch is a superseded iteration; disposition abandon-with-summary, no code change.
- CHANGELOG.md — Documentation entry under Unreleased pointing at the triage report.

## Verification (commands run + pass/fail)
- `git log --oneline -5 hydra/wt-hydra-242b` — tip 9945368, feature commit c3f3500 (escalation auto-expiry, overseer.escalation_expiry_hours 48h).
- `git log --oneline origin/development --grep=escalation` + `git show 2ee7449 --stat` — landed escalation_ttl scanner (default 72h) supersedes c3f3500. PASS.
- `git diff origin/development..hydra/wt-hydra-242b --stat` — 61 files, +272/−3497; deletes internal/triage/*, internal/degrade/* (newer landed work) → rebase/cherry-pick rejected. VERIFIED.
- `git merge-tree --write-tree origin/development hydra/wt-hydra-242b` — conflicts only in docs/hydra/handoff/task-242b.md (add/add) and internal/core/config.go (TriageConfig churn). No substantive wedge.
- Existing exhausted-cap code located: internal/core/{bead_closer.go,config.go,service_starter.go,integration_exhausted_gate_test.go,integration_retest.go}. No code change made (discovery bead).

## Open edges for the next phase
- Action for human/Pan: close hydra-242b with summary (abandon-with-summary). Do NOT rebase, cherry-pick, or clear-hard-park.
- Retry-budget knob proposal REJECTED per triage report: durable park + triage path worked as designed (d778, 014y, 242b all superseded iterations).
- Note: this worktree's `br show` fails with "Error: Beads not initialized: run 'br init' first" — bead state inspected via git history + prior handoffs instead; not a blocker for this doc-only change.
