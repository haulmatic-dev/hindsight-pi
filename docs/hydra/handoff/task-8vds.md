# refactor handoff (task-8vds)

## What changed (files + one line each)
- internal/core/gate_deps_preflight.go: `gateDepsPreflight` decomposed into two named helpers — `depsInstallRequired(worktreePath)` (manifest-present + modules-missing predicate) and `installNodeDeps(worktreePath, run)` (lockfile-detected install + tree-clean guard); entrypoint is now a 4-line read: predicate → install-or-fail → run verification verbatim. Error strings, execution order (install → `git status --porcelain` guard → verification command), and returned output byte-identical.
- CHANGELOG.md: Unreleased/Changed entry for the decomposition.
- internal/core/gate_deps_preflight_test.go: untouched by design — the 5 contract tests pin the parent AC verbatim; their per-scenario fixture setup is scenario-specific (no duplication worth a shared fixture helper without hurting contract readability). All 5 pass unchanged.
- docs/operations/host-setup.md: line 310 knob row verified still accurate post-refactor (behavior and file names unchanged); no edit required.
- docs/hydra/handoff/task-8vds.md: this handoff.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit baseline): `go test ./internal/core/ -run 'TestGateDeps_' -count=1 -v` — PASS, 5/5 `--- PASS` (InstallWhenManifestMissingNodeModules, NoManifestSkipsInstallAndRunsVerification, InstallDirtyingTrackedFilesFails, VerificationOutputPreservesPASSLines, PresentNodeModulesSkipsInstall), ok 0.045s.
- AFTER: same command — PASS, 5/5, ok 0.063s. No `[no tests to run]`.
- `gofmt -l internal/core/gate_deps_preflight.go` — clean (empty).
- `go vet ./internal/core/` — clean (no output).
- No benchmarks exist for the deps-preflight path (prior chain handoffs confirm); no perf check required.

## Open edges for the next phase
- `gateDepsPreflight` has no production call site on this branch yet — the one-line wiring inside `gateExecutableAcceptance` (internal/core/bead_closer_hard_gates.go) belongs to the FR-001 feature bead's edit budget per the task's target-files note; the refactor did not claim it.
- Parent criterion "Install that dirties tracked files → treated as failure" remains pinned by TestGateDeps_InstallDirtyingTrackedFilesFails (real git repo fixture, tracked.txt dirtied by the install step, verification command asserted never invoked).
