# fix handoff (task-cw35)

## What changed (files + one line each)
- internal/core/bead_closer.go: new `integrationPendingLabel` ("integration-pending"); `failIntegration` no longer reopens the bead to `open` — work-complete bead stays parked in place (in_progress) with `integration-failed` + `integration-pending` labels; exhaustion cap (integration-exhausted at 3 consecutive failures) unchanged; rapid-fire contention guard and backoff unchanged.
- internal/core/scheduler.go: retry queue skips `integration-pending`; `reconcileOrphanClaim` hard-park label list gains it.
- internal/core/dag_navigator.go: `GetReadyBeads` skips `integration-pending`.
- internal/core/bead_discovery.go: `filterByDAG` skips `integration-pending`.
- internal/core/charon.go: OrphanRetry skips `integration-pending` (no orphan reopen → no worker respawn).
- internal/core/integration_retest.go: retest selector selects pending beads; land path strips `integration-pending` alongside the other integration labels.
- internal/core/pan_health.go: digest `OrphanClaims` counter excludes pending beads (they are in_progress with no worker by design).
- internal/core/service_starter.go: `panBlockedReason` reports pending parks ("work complete, integrate/push failed ... retest stage retries mechanically").
- internal/core/integration_pending_test.go (new): pending-park semantics (no reopen, labels, no cap trip on 1 failure, retest selector selects pending with live hydra/wt-* branch).
- internal/core/bead_closer_singleton_integration_test.go: post-integrate-failure expectation updated — bead stays in_progress with integration-pending label (was: reopened to open).
- internal/core/integration_loop_test.go, integration_retest_test.go: expectations updated (in_progress not open; 3 labels stripped on land).
- CHANGELOG.md (Fixed), docs/architecture/shepherd.md (integration-pending documented in blocked-beads + retest-stage sections).

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS. `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on all touched files — clean.
- `go test ./internal/core/ -run 'TestFailIntegration|TestIntegrationSuccess|TestRetestSelector' -count=1 -v` — PASS (8 passed incl. 3 new; log /tmp/n4.log).
- `go test ./internal/core/ -run 'TestOrphanClaim|TestRecoverOrphan|TestCollectHealthDigest|TestGetReady|TestFilterByDAG|TestEscalated|TestExhausted' -count=1 -v` — PASS (23 passed, 0 FAIL; log /tmp/n5.log, 313s).
- `go test ./internal/core/ -run 'TestPanBlockedReason|TestFailIntegration|TestIntegrationRetest|TestRetestSelector' -count=1 -v` — PASS (10 passed, 0 FAIL).
- `go test ./internal/core/ -run 'TestBeadCloser_IntegrateErrorFailsClose' -count=1` — PASS (daemon full-suite gate failure fixed: singleton close test expected the old reopen-to-open behavior).
- Commit 7fd6939 + b9d979d verified via `git show --stat`.

## Open edges for the next phase
- If `integration_retest` is disabled in a project config, a pending bead has no mechanical retry path (only Pan escalation / exhaustion cap). Deployment uses retest (enabled), but a config knob guarding pending parks against retest-disabled configs was deliberately NOT added (smallest change); revisit if retest gets disabled fleet-wide.
- Pan `clear-hard-park` still refuses pending-only beads ("carries no hard-park label") — the exhaustion cap adds `integration-exhausted` at 3 failures, which is the Pan lever. If single-failure pending parks ever need manual clearing, extend clear-hard-park to also accept `integration-pending`.
- The bead description's "403" trigger was not special-cased; the pending park applies to ALL integrate/push failure classes uniformly (conflicts included — retest stage distinguishes via merge-tree).
