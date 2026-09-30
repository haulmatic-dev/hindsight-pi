# fix handoff (task-5g6e)

## What changed (files + one line each)
- internal/core/integration_gate.go: adaptive per-repo cooldown — `integrationRepoFailStreak` counts consecutive gated failures per repo; each failure doubles the next sibling's wait (30s→1m→2m→4m→8m, capped at `integrationBackoffMax`); any success (in-gate or via `resetIntegrationCooldown`) resets streak to base 30s. Implements the proposed stagger/queue-backoff for concurrent exhaustion bursts.
- internal/core/integration_gate_test.go: `TestIntegrationGate_EscalatingCooldown` (base→2x→4x waits, no cross-repo streak leak), `TestIntegrationGate_SuccessResetsStreak` (success clears streak; next failure counts 1).
- CHANGELOG.md: Fixed entry under Unreleased.
- docs/architecture/shepherd.md: per-repo integration gate section documents the adaptive cooldown.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -run TestIntegrationGate -count=1` — PASS (6 tests: serialize, repo-independence, base cooldown, escalating cooldown, streak reset, success clears).

## Open edges for the next phase
- Prior phases already landed the rest of the proposed remedy: per-repo serialization + cooldown (integration_gate), per-bead exponential backoff with rapid-fire contention guard (failIntegration), integration_retest + fast-path absorption, PR-reuse (`resolveOrCreatePR`), post-merge state-read retry. The storm cohort itself: xxq5 = poisoned br WAL index (recovered, triage 2vuc); 5ob8 = PR-already-exists proposal landed (triage fhhy); mhk2 = publisher wedge tracked, jcj6 closed as its dedupe twin. Parks are durable by design — no clear needed.
- Adaptive streak is in-memory only (like the rest of the gate); daemon restart resets to base cooldown — acceptable, matches existing design.
