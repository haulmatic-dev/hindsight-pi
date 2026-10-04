# green handoff

## What changed (files + one line each)
- internal/core/bead_closer_hard_gates.go (new): `runCloseHardGates` — Gate 1 executable acceptance (extracts fenced **Verification Command:** block via `extractVerificationCommand`, runs in worktree/project dir via /bin/sh, non-zero exit → `rejectViaGate` reason `verification-failed` with verbatim CombinedOutput in the `close_gate_rejected` event; no command → fail-open + `verification_command_missing` event); Gate 2 quarantine contradiction (`closed-vs-quarantined` when `refs/heads/hydra/quarantine/<id>` exists and bead lacks `integrated` label; git rev-parse errors fail open); Gate 3 dependency order (chain-member titles RED:/GREEN:/REFACTOR:/SYNTAX: → `dep-open` rejection when any dependency is non-terminal, fetch errors fail open; singletons → warn-only `close_dep_open` event).
- internal/core/bead_closer.go: hard gates wired on the completed-close path (success && reason=="completed") before the VerifiedClose judge round; all rejections route through existing `rejectClose`, so reopen → attempt cap → escalate semantics are untouched.
- internal/core/bead_closer_hard_gates_test.go: cherry-picked RED suite (commit b06d73e) — 10 tests.
- CHANGELOG.md: GREEN entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestCloseGate|TestQuarantineClose|TestCloseDep|TestVerifiedClose_HardGate' -count=1` — PASS (10/10).
- Regression: `go test ./internal/core/ -run 'TestVerifiedClose|TestBeadCloser|TestClose|TestPersistClosedEpics|TestWorktree' -count=1` — PASS.
- `go vet ./internal/core/` — PASS (clean); `gofmt -l` on touched files — clean.
- Coverage (new impl, gated-test profile): runCloseHardGates 85.7%, rejectViaGate 100%, extractVerificationCommand 81.2%, gateExecutableAcceptance 91.7%, gateQuarantineContradiction 90.0%, isChainTitle 100%, gateDependencyOrder 94.1% (~90% overall).

## Open edges for the next phase
- `Close` wiring passes worktreePath="" — executable acceptance currently runs in the project dir, not the chain worktree; when the worktree manager is reachable on this path the dir should be threaded through (tests pin only the direct call shape).
- Gate 3 `close_dep_open` singleton event Detail carries "warn" prefix (case-insensitive match in test); keep if event text is ever refactored.
