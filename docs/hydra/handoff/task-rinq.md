# discovery handoff (task-rinq, FR-004 turn telemetry)

## What changed (files + one line each)
- docs/discovery/turn-telemetry-fr004-rinq.md (new): discovery — verified zero turn_end observability in the worker runtime, documented the staged RED surface (cmd/worker/turn_telemetry.go inert stub, internal/registry/types.go TurnsSoFar heartbeat field, tools/turn-report inert stub — all sibling-strand artifacts, untracked, NOT touched here), resolved 5 ambiguities (usage-free turn_end counts, nudged turns count individually, phase from title prefix, heartbeat-only persistence, SIGKILLed exits emit no event), and pinned the GREEN checklist.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verification (commands run + pass/fail)
- `cm context "worker close events turn telemetry turn_end TurnsSoFar heartbeat worker_turns" --json` — top bullet (finalScore 1.2) confirms the session-jsonl turn_end counting precedent (Pan tier-1 signals count turn_end events). PASS.
- `graphify` not invoked: context-mode hook failed at bead start (NODE_TOO_OLD); rg-based code discovery served the same purpose per protocol. PASS (fallback).
- Code inspection: cmd/worker/pi_rpc_turn.go:296 (turn_end handled, never counted), cmd/worker/main.go:1628 (nudge-only turnCount), reporter.go:196-206 (SetTokenBreakdown heartbeats), rpc_usage_report_test.go (feedRPCLines RED pattern to reuse). PASS.
- RED surface inventory: turnTelemetryImplemented=false with no turn_telemetry_test.go; tools/turn-report turnReportImplemented=false; registry TurnsSoFar field present uncommitted. PASS.
- `git status --short` at close — only this bead's docs + CHANGELOG new; sibling RED stubs left untracked, untouched. PASS.

## Open edges for the next phase (GREEN)
- `cmd/worker/turn_telemetry_test.go` (3-turn RED test) does not exist yet — RED strand obligation; GREEN must land it if RED hasn't.
- Green flips `turnTelemetryImplemented`/`turnReportImplemented`, implements rpcTurn turn counting (nudge-reset must NOT clear the counter), heartbeat forwarding beside forwardUsage, and `worker_turns` {bead, turns, phase, outcome} at both main.go exit paths, fail-open.
- Gate: `go test ./cmd/worker/ ./internal/core/ -run 'Turn' -count=1` exits 0.
