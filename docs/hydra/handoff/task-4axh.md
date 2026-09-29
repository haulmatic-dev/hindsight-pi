# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-4axh.md: triage of the fleet-wide integration-exhausted shared wedge — both proposed remedies (PR-exists detection before create; sequential rebase-integrate with project-level concurrency circuit breaker) are already landed; no code change.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verification (commands run + pass/fail)
- `rg isPRAlreadyExists|resolveOrCreatePR internal/core/publisher.go` — classifier (~300) + reuse helper (~659) present, used by both PR publish paths (~330, ~419). PASS.
- `internal/core/integration_gate.go` — per-repo keyed mutex + bounded cooldown, motivation comment cites the telematics parallel-CHANGELOG wedge. Wired at bead_closer.go:316 + worktree_reconciler IntegrateBranchAndRemove. PASS.
- Daemon process: `/opt/dev-tools/var/hydra/bin/hydra -config /etc/hydra/hydra.yaml` started 2026-09-29T16:57, binary mtime 16:56:58 → fixes deployed. PASS.
- Events 2026-09-29.jsonl: 434 integration-exhausted mentions on 09-29, but 0 `integration_exhausted` events timestamped ≥17:00 (post-restart). Last pre-restart parks (8dvj, oive, 5l11, publish-hydra PR #394 collision) all ≤16:50 on the old binary. PASS.
- graphify-out/graph.json absent in cwd → rg fallback per protocol.

## Open edges for the next phase
- Residual hard-parks predate deploy, durable by design: clear per prior per-bead triages (abandon-with-summary for superseded branches) or batch via operator unpark API after cause verification.
- Tier B agentic conflict resolution remains unimplemented — separate bead.
- If parks recur post-deploy, re-triage with fresh events.
