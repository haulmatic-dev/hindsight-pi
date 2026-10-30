# green handoff (task-6gnv)

## What changed (files + one line each)
- internal/core/gate_deps_preflight.go: `depsPreflightImplemented = true`; `gateDepsPreflight(worktreePath, command, run)` — when `package.json` exists and `node_modules` is absent, runs `<manager> install` in the worktree (manager via existing `detectNodeManager`); install error → wrapped error; then the `worktreeDirtyAfterInstall` guard (real `git -C dir status --porcelain`; any git error incl. non-repo dirs → "" = fail-open) → non-empty porcelain output → error; then runs the verification command verbatim via the injected `run` seam.
- docs/operations/host-setup.md: knob row updated — removed the "(GATE_DEPS_ENFORCE=1 until the GREEN phase lands)" clause; now points at the implemented file.
- CHANGELOG.md: Added entry under Unreleased for the GREEN implementation.
- docs/hydra/handoff/task-6gnv.md: this handoff.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestGateDeps_' -count=1 -v` — PASS, exit 0: 5/5 `--- PASS` (InstallWhenManifestMissingNodeModules, NoManifestSkipsInstallAndRunsVerification, InstallDirtyingTrackedFilesFails, VerificationOutputPreservesPASSLines, PresentNodeModulesSkipsInstall), `ok github.com/haulmatic-dev/hydra/internal/core 0.063s`; no `[no tests to run]`.
- `go test ./internal/core/ -run 'TestGateDeps_' -count=1 -coverprofile` — PASS; coverage on new code: `gateDepsPreflight` 85.7%, `worktreeDirtyAfterInstall` 100.0%.
- `go build ./...` — PASS (exit 0).
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l internal/core/gate_deps_preflight.go` — clean.
- Placeholder grep (TODO/FIXME/Placeholder) over gate_deps_preflight.go — 0 matches; file non-empty.
- The one-line call site inside `gateExecutableAcceptance` (bead_closer_hard_gates.go) was NOT touched — belongs to FR-001's edit budget per bead metadata.

## Open edges for the next phase
- Wiring into `gateExecutableAcceptance` (single-line `gateDepsPreflight(...)` call) lands with FR-001; until then the helper is compiled + tested but not invoked by the close path.
- `detectNodeManager` returns ("npm", "") when no lockfile matches, so bare manifest fixtures install via `npm install` — expected contract, documented in the test.
- Fail-open on git faults (non-repo worktree dirs) is deliberate: a broken `git` must not block every Node close.
