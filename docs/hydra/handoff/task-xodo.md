# syntax handoff (SYNTAX: Check Deps preflight — companion to FR-001, same chain)

## What changed

None. Verify-only close: `internal/core/gate_deps_preflight.go`,
`internal/core/gate_deps_preflight_test.go` and the
`docs/operations/host-setup.md` gate-deps knob row (line 310) already conform
to all SYNTAX acceptance criteria. No code or doc edits required.

## Verification

- `go test ./internal/core/ -run 'TestGateDeps_' -count=1 -v` — PASS 5/5
  (InstallWhenManifestMissingNodeModules, NoManifestSkipsInstallAndRunsVerification,
  InstallDirtyingTrackedFilesFails, VerificationOutputPreservesPASSLines,
  PresentNodeModulesSkipsInstall); `ok ... 0.027s`.
- `go build ./...` — exit 0.
- `go vet ./...` — exit 0.
- `gofmt -l` on both target files — no output (clean).
- Placeholder grep (TODO/FIXME/Placeholder) — 0 hits in both files.
- Doc comments verified on `gateDepsPreflight`, `depsInstallRequired`,
  `installNodeDeps`; `depsPreflightImplemented = true` at line 33.
- host-setup.md gate-deps row present (line 310).

## Open edges

- `gateDepsPreflight` still has no production call site; the one-line wiring
  inside `gateExecutableAcceptance` (internal/core/bead_closer_hard_gates.go)
  belongs to FR-001's GREEN edit budget per the bead target-files note.
- Pre-existing stray ` M .gitignore` in the worktree, untouched by this bead.
