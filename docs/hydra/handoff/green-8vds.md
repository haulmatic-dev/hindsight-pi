# green handoff (green-8vds)

## What changed (files + one line each)
- internal/core/beadfiles_adoption.go (new): `beadTargetFiles` delegates to the shared `internal/beadfiles.Resolve` (single parser — metadata `files`/`target_files`, Context JSON block, description files lines; `:symbol`/`TBD` normalized) plus one-directional sibling-test expansion (`siblingTestExpansion`/`siblingTestPath`): every resolved impl `x.go` gains `x_test.go` in scope; test-only declarations gain no impl scope.
- internal/core/bead_closer.go: removed the bead-local `beadTargetFiles`/`splitFileList` parser (replaced by the adoption file); pointer comment left at the old site.
- internal/core/bead_closer_adoption_test.go → internal/core/beadfiles_adoption_test.go (new): resolver unit tests (expansion matrix, nil, TBD) + one `TestBeadfilesAdoption_*` regression test per behavior-changed caller: scope gate `hasTargetChanges` (sibling-test-only diff now in-scope; test-only declaration stays empty-diff), prespawn probe, target overlap (sibling test of a live worker's target conflicts), satisfied close, fix audit, integration retest, daemon close gates (the four scope-passing callers pinned on `:symbol` normalization reaching `<scope>` substitution).
- internal/core/bead_closer_verifiedclose_test.go: `TestBeadTargetFiles` table updated to the new expected verdicts (`.go` impl files count their sibling tests: 2→4, 1→2).
- CHANGELOG.md: Changed entry under Unreleased.

## Verification (commands run + pass/fail)
- Commit provenance: shared worktree — the four Go files were swept into the concurrent strand's commit 03bfa8dd (bead hydra-nvdm, 17:32 UTC) as-is; this bead's commit b76b0cd3 carries CHANGELOG.md + docs/hydra/handoff/green-8vds.md. Both commits on HEAD; tree clean; disk == HEAD.
- `go test ./internal/core/ -run 'TestBeadfilesAdoption_' -count=1 -v` — PASS: 15 `--- PASS`, 0 `--- FAIL`, `ok github.com/haulmatic-dev/hydra/internal/core 0.035s` (bash-tool interceptor masks go-test stdout; log file authoritative).
- `go build ./...` — PASS (`[OK] Build successful`).
- `go test ./internal/core/ -run 'TestBeadTargetFiles|TestPrespawnProbe|TestTargetsOverlap|TestTargetScopeConflict|TestBuildCloseSatisfied|TestBuildFixVerifier|TestDaemonGates|TestVerifyBeforeClose|TestCollectCloseEvidence|TestEmptyDiff|TestHasTargetChanges|TestScopeViolations|TestTargetless' -count=1` — PASS (`ok ... 3.143s`): no ripple regressions in the affected caller suites.
- `go vet ./internal/core/` — clean. `gofmt -l` on touched files — clean.
- Coverage (`-coverprofile` over `TestBeadfilesAdoption_|TestBeadTargetFiles`): beadTargetFiles 100.0%, siblingTestExpansion 100.0%, siblingTestPath 100.0%.
- Placeholder grep over new files — 0 hits.

## Open edges for the next phase
- `splitFileList` in internal/beadfiles remains package-private and duplicated only conceptually (both packages now share `Resolve`; core no longer has its own parser).
- Scope-passing callers (prespawn/satisfied/fix-audit/retest/gate-close) keep `targets[0]` as gate scope — sibling tests are appended AFTER impl files, so scoped-gate scope is unchanged; pinned by TestBeadfilesAdoption_PrespawnProbe_CleanScopeProbes.
- `targetlessWarningPaths`/`scopeViolations` companion-test allowance in scope_gate.go predates this change and still works (expansion is additive).
