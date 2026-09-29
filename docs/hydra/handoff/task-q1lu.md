# green handoff

## What changed (files + one line each)
- internal/pan/pan.go: repairWhitelist gains `clear-escalation`; judgment prompt lists it among valid repair names and adds a stale-escalation recipe (escalation >~2 days with no human response and no live worker → clear-escalation for one fair retry; obsolete work → abandon-with-summary; <2 days → leave blocked).
- internal/core/service_starter.go: new `clear-escalation` repair handler — resolves project, GetBead, refuses unless the `escalated` label is present, removes `escalated`/`needs-human`/`verified-close-escalated`, reopens to StatusOpen if needed, resets retry counter (one fair retry; escalation re-fires on recurrence).
- internal/pan/pan_test.go, internal/pan/file_bead_test.go: whitelist/prompt tests now cover `clear-escalation`; added TestBuildPromptEscalationRecipe.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/pan/ ./internal/core/` — PASS.
- `go test ./internal/pan/ -count=1` — PASS.
- `go test ./internal/core/ -count=1` — PASS.

## Open edges for the next phase
- Live pending escalations (hydra-wdp group, hydra-cip, hydra-gvh, hydra-ol7) are NOT auto-cleared by this change — a Pan judgment tick must choose `repair:clear-escalation`, or an operator clears labels manually.
- Repair efficacy gate still applies: repeated clear-escalation on an unchanged condition is refused as human-only after maxConsecutiveRepairs.
