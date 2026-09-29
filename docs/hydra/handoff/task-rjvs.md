# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-triage-ycmu.md: triage of hydra-ycmu integration-exhausted — escalation-aging branch superseded by landed escalation_ttl; disposition abandon-with-summary.
- CHANGELOG.md: Documentation entry under Unreleased.
- No code changes (triage-only discovery bead).

## Verification (commands run + pass/fail)
- `git log origin/development..hydra/wt-hydra-ycmu` — 1 unique commit 7be95d1 (escalation-aging policy, 481 insertions) + integration commit e4fe245. PASS.
- `git cat-file -e origin/development:internal/core/escalation_aging.go` — NOT_LANDED.
- `git log origin/development -- internal/core/escalation_ttl.go` — 2ee7449 landed (same problem domain, simpler 72h expire-to-ready). PASS.
- `git diff origin/development..hydra/wt-hydra-ycmu -- internal/core/escalation_ttl.go` — branch DELETES landed TTL scanner (-139). Full diff: 78 files +539/−3998 (deletes internal/triage/* etc.) → rebase = revert newer work; cherry-pick = duplicate scanner racing escalation_ttl + conflicts in overseer.go/service_starter.go/host-setup.md (merge-tree CONFLICT ×3). PASS.
- Fleet-level bead hydra-10ka CLOSED (PR #364); per-bead abandon-with-summary pattern established. PASS.

## Open edges for the next phase
- Human/Pan: `br close hydra-ycmu -r "not_planned: superseded by escalation_ttl (2ee7449)"`; delete branch after closure. Do NOT clear hard-park labels.
- If re-notify/channel stages wanted later, extend escalation_ttl.go — never a second scanner.
