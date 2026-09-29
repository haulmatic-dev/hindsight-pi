# FEATURE handoff

## What changed (files + one line each)
- internal/core/integration_gate.go: `SetIntegrationOpenNotifier(fn)` — package-level hook fired (under mutex, fail-open) from `resetIntegrationCooldown` whenever a gated integration succeeds, so the retest stage learns the repo's integration path just opened.
- internal/core/integration_retest.go: `IntegrationRetestStage.NotifyRepoOpen(projectPath, projects)` — fast-path absorption: opens a 5-minute fast-path window (`retestFastPathWindow`) for the repo and runs an immediate background sweep (serialized per repo via `sweeping` flag; unknown repo path → no window, no sweep; nil/disabled stage inert). `allowAttempt(projectPath, beadID)` now takes repoPath and bypasses the per-bead `RetryInterval` while the window is open; `MaxAttempts` cap always applies.
- internal/core/overseer.go: NewOverseer registers `SetIntegrationOpenNotifier` after the retest stage is constructed (closes over `o.projects` snapshot each fire).
- internal/core/integration_retest_test.go: `allowAttempt` call sites updated for new signature.
- internal/core/integration_retest_fastpath_test.go: 4 tests — interval bypass inside window + cap still enforced + expired window + per-repo scoping; notifier fire-on-success + clear; NotifyRepoOpen sweep end-to-end with fake BeadsClient + unknown-repo/no-window + nil-stage inert; window constant bounds.
- CHANGELOG.md, docs/architecture/shepherd.md: operator-facing entries.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'IntegrationRetest|FastPath|OpenNotifier|AllowAttempt|SelectBead|IntegrationGate|QuarantineReattempt' -count=1` — PASS (ok 0.984s).
- `go test ./internal/core/ -count=1` — PASS (ok 64.116s, full package).
- `gofmt -l` on touched files — clean.

## Open edges for the next phase
- Fast-path sweep uses `runProject(p, nil)` — activeKeys nil means a bead mid-flight under a live worker is still guarded by `selectBead`'s terminal/park-label checks, but the overseer-tick path passes `o.activeWorkerKeys()`; consider caching an active-keys snapshot on the stage if double-attempt risk on in-flight beads ever shows.
- Window length (5m) and per-repo serialization are constants; a knob (`integration_retest.fast_path_window`) could be added if operators need tuning.
- Tier B agentic conflict resolution still unimplemented (separate bead); fast-path only absorbs beads whose conflict evaporated — it cannot land still-conflicted branches.
