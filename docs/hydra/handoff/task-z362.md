# green handoff

## What changed (files + one line each)
- internal/core/integration_gate.go (new): per-repo integration gate — keyed mutex serializes integrate/PR attempts per repo; after a gated failure sibling beads wait a 30s repo cooldown (success clears it); `errStage` preserves commit/integrate/push stage through the gate.
- internal/core/bead_closer.go: singleton close-path integration (publishSingletonViaPR / Integrate / publishAfterIntegrate) now runs under `integrationGate(projectPath, ...)`; cooldown reset on success; failure still routed to failIntegration with original stage.
- internal/core/worktree_reconciler.go: reconciler's `IntegrateBranchAndRemove` (stale-branch merge back to base) also gated per repo.
- internal/core/quarantine_reattempt.go (new): `QuarantineReattemptRepair` — Pan repair `retry-quarantine`: requires hydra/quarantine/<key>; merge-tree vs origin/<base> (local base fallback); still-conflicted → fail-closed refusal; clean → restore hydra/wt-<key>, delete quarantine branch, remove stranded-conflict, reopen, emit `quarantine_reattempt{result:requeued}`.
- internal/core/service_starter.go: wired `retry-quarantine` verb (projectPathForID + chainKeyForBead + retry reset).
- internal/pan/pan.go: whitelist + prompt list + rescue recipe (prefer retry-quarantine over clear-hard-park for stranded-conflict).
- internal/core/integration_gate_test.go, quarantine_reattempt_test.go: serialization/cooldown tests; real-git re-attempt tests (clean requeue incl. label/status/event, still-conflicted refusal preserving branch, no-branch refusal).
- CHANGELOG.md (Added), docs/architecture/shepherd.md (verb table row + gate paragraph).

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/ ./internal/pan/` — PASS (clean).
- `go test ./internal/core/ -run 'TestIntegrationGate|TestQuarantineReattempt' -count=1` — PASS (7 tests).
- `go test ./internal/pan/ -count=1` — PASS (92 tests).
- `go test ./internal/core/ -count=1` — PASS (1197 tests, 59s).

## Open edges for the next phase
- The 4 live integration-exhausted beads need per-bead triage: branches may be superseded (per task-242b/d778/014y pattern) — check merge-tree against origin before any clear-hard-park/retry-quarantine.
- Repo cooldown (30s) is a constant mirroring integrationBackoffBase; not config-tunable — add a knob only if live evidence demands it.
- Quarantine requeue relies on the reconciler/retest stage to land the restored branch; the restored branch merges clean per merge-tree but gates re-run in those paths.
