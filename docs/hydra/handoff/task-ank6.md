# refactor handoff (refactor-ank6)

## What changed (files + one line each)
- internal/core/bead_closer_hard_gates.go: 3 duplicated `events.EmitRepo` blocks (rejection event, verification_command_missing, close_dep_open) unified into one `emitGateEvent` helper; `closeDepOpenEvent` named constant; dependency-order gate restructured singleton-first. No behavior change.
- CHANGELOG.md: Unreleased/Changed entry.
- cm add recorded: `gateExecutableAcceptance` never sets `cmd.Dir` — verification command executes in daemon cwd, not worktree/projectPath (tests use cwd-independent commands, so untested). Bug recorded per REFACTOR discipline, NOT fixed here.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestCloseGate|TestQuarantineClose|TestCloseDep|TestVerifiedClose_HardGate' -count=1` — PASS (both before and after edits).
- `go test ./internal/core/ -run 'TestClose|TestBeadCloser|TestVerifiedClose|TestTerminalGuard' -count=1` — PASS (close-path regression set).
- `gofmt -l internal/core/bead_closer_hard_gates.go` — clean. `go vet ./internal/core/` — clean.

## Open edges for the next phase
- BUG (recorded, not fixed): hard gate 1 should execute the verification command with `cmd.Dir = dir` (worktreePath fallback projectPath); currently runs in daemon cwd. GREEN/fix bead should add a test pinning cwd (e.g. command `pwd`) and set Dir.
- No benchmarks exist for this functionality.
