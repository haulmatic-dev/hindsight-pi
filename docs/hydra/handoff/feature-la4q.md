# FEATURE handoff

## What changed (files + one line each)
- internal/core/escalation_ttl.go: new EscalationTTLScanner — periodic (1h + at startup) demotion of non-terminal beads whose `escalated` label aged past TTL: label swapped for `escalation-expired`, typed `escalation_expired` event; fail-open, nil-safe, in_progress skipped.
- internal/core/config.go: `overseer.escalation_ttl` (default 72h in DefaultConfig, 0 disables).
- internal/core/overseer.go: scanner field + construction (when TTL > 0) with o.GetProjects + eventSink.
- internal/core/service_starter.go: Start/Stop wiring alongside orphan janitor.
- internal/core/escalation_ttl_test.go: 4 tests (demote stale, skip fresh/in_progress/terminal/unlabeled, disabled-at-0, nil-safe event path).
- CHANGELOG.md, hydra-config.yaml.example, docs/architecture/shepherd.md: operator docs.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go test ./internal/core/ -run TestEscalationTTL -count=1` — PASS.
- `go test ./internal/core/ -count=1` (full package) — PASS (52.8s).
- `gofmt` clean, `go vet ./internal/core/` — PASS.

## Open edges for the next phase
- Pan repair verbs: no verb clears `escalation-expired`; if a demoted bead loops the same death, human/Pan must handle — consider allowing a Pan verb to re-escalate.
- Scanner uses UpdatedAt as escalation age proxy (no dedicated escalation timestamp); any label touch resets the clock (conservative direction).
- Live e2e on the stale beads (hydra-wdp/cip/gvh/ol7) after deploy: confirm one scan cycle demotes them and they respawn.
