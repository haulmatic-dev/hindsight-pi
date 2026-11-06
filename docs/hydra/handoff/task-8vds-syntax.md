# syntax handoff (deps preflight, companion to FR-001)

## What changed (files + one line each)
- None (verification-only SYNTAX pass). Implementation and tests already conform; no edits required.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestGateDeps_' -count=1 -v` — PASS, exit 0: 5/5 `--- PASS` (InstallWhenManifestMissingNodeModules, NoManifestSkipsInstallAndRunsVerification, InstallDirtyingTrackedFilesFails, VerificationOutputPreservesPASSLines, PresentNodeModulesSkipsInstall); no `[no tests to run]`.
- `go vet ./internal/core/` — PASS (clean). `go build ./...` — PASS (BUILD_OK).
- `gofmt -l internal/core/gate_deps_preflight.go internal/core/gate_deps_preflight_test.go` — clean.
- Placeholder grep (`TODO|FIXME|Placeholder`) over gate_deps_preflight.go + test — 0 hits; both files non-empty (3,470 B / 8,022 B).
- Doc comments present on `gateDepsPreflight`, `depsInstallRequired`, `installNodeDeps` (unexported but documented).
- `docs/operations/host-setup.md` carries the gate-deps knob row (1 match).

## Open edges for the next phase
- `gateDepsPreflight` is implemented with `depsPreflightImplemented = true` but has NO production call site yet — the one-line call inside `gateExecutableAcceptance` (internal/core/bead_closer_hard_gates.go) belongs to the first requirement's (FR-001 GREEN) edit budget per the bead's target-files note. If FR-001's GREEN bead is already closed, that wiring gap must be refiled; otherwise it is expected there.
