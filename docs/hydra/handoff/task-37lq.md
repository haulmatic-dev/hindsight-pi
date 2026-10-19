# task handoff (hydra-37lq)

## What changed (files + one line each)
- internal/core/integration_retest.go: `chainKeyFn` field + `SetChainKeyForBeadID` setter + constructor param; `findBranch` chain-root fallback (when the bead's own hydra/wt-<id>[-vN] / hydra/quarantine/<id> branches are gone, retries with the chain root's key — same hydra-owned prefixes and versioned -vN / ls-remote handling); landed-check via commit ancestry: after `fetch origin <base>`, a branch tip already an ancestor of `origin/<base>` closes the bead via `closeLanded` (no PR — a PR here would carry an empty diff and fail the rdju no-diff class).
- internal/core/quarantine_reconciler.go: after successful re-integration, `resetIntegrationCooldown(e.ProjectPath)` resets the per-repo integration cooldown AND fires the integration-open notifier, so the retest stage's fast-path absorption sweeps parked siblings (incl. chain members via the fallback) immediately instead of waiting out the hourly RetryInterval.
- internal/core/overseer.go: `NewIntegrationRetestStage(..., o.chainKeyForBeadID)` — chain-aware selector wiring at construction.
- internal/core/integration_retest_test.go, internal/core/integration_pending_test.go: call sites updated for the new constructor signature (nil chainKeyFn = own-branch-only selector, prior behavior).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS (clean).
- `gofmt -l internal/core/` — clean.
- `go test ./internal/core/ -run 'TestRetestSelector|TestIntegrationRetest|TestQuarantineReconciler' -count=1` — PASS (ok, 1.391s).
- Commit 6fbb9b1d verified via `git show --stat` (6 files, +103/−27).

## Open edges for the next phase
- `closeLanded` closes via the raw beadsClient (no verified-close hard gates / terminality recheck) — acceptable because the branch tip is provably contained in origin/<base> at close time; if CloseBead fails (open children/blockers), the error logs and the bead stays parked for the next tick.
- After a chain root lands via PR, its `hydra/wt-<key>` branch is not deleted by the retest path; parked siblings on that branch then close via the ancestry landed-check on subsequent sweeps (this is the intended chain-aware drain; branch cleanup relies on the publish/integration path).
- Chain-root fallback is a no-op for singleton beads: `o.chainKeyForBeadID` returns the bead's own ID when it belongs to no registered chain.
