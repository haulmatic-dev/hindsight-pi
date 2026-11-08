# red handoff

## What changed (files + one line each)
- internal/core/bead_closer_hard_gates_test.go (new): 10 RED tests for FR-002 "Close-time hard gates in the bead closer" — gate 1 executable acceptance (fail → reject `verification-failed` with verbatim output in `close_gate_rejected` event Detail; pass → no rejection; missing command → fail-open + `verification_command_missing` event), gate 2 quarantine contradiction (`hydra/quarantine/<beadID>` branch + no `integrated` label → reject `closed-vs-quarantined` with branch evidence; integrated → pass; no branch → pass), gate 3 dependency-order (chain member with non-terminal dependency → reject `dep-open` + `close_dep_open` event carrying the dep ID; terminal dep → pass; singleton → warn-only `close_dep_open` with no rejection), plus `TestVerifiedClose_HardGateRejectionFollowsRoundCapAndEscalates` pinning unchanged reject→reopen→attempt-cap→escalate semantics (retry counted, `needs-human`+`verified-close-escalated` labels, one `bead.escalated` event).
- CHANGELOG.md: Unreleased/Added entry.
- Contract pinned for GREEN: entry point `(*BeadCloser).runCloseHardGates(bead, projectPath, repoName, worktreePath) error`, called before the VerifiedClose judge round; rejection events typed `close_gate_rejected` with Reason slugs `verification-failed` / `closed-vs-quarantined` / `dep-open` and evidence in `Detail`; chain member = RED/GREEN/REFACTOR/SYNTAX title prefix.

## Verification (commands run + pass/fail)
- `go vet ./internal/core/` — RED CONFIRMED: `vet: internal/core/bead_closer_hard_gates_test.go:83:11: c.runCloseHardGates undefined (type *BeadCloser has no field or method runCloseHardGates)` + 10 further `undefined` errors, one per test. FAIL for the RIGHT reason: the gate entry point (the feature) does not exist. Not a typo/compile error in the test itself — every other symbol resolves (`go vet` stops only on the 11 missing-method errors, all the same name).
- `go test ./internal/core/ -run 'TestCloseGate|TestCloseDep|TestQuarantineClose' -count=1` — FAIL [build failed], same `runCloseHardGates undefined` (quoted above).
- `gofmt -l internal/core/bead_closer_hard_gates_test.go` — clean (formatted once after initial write).
- Coverage for new code: 0% — the gate code is not implemented (RED phase); the 10 tests are the coverage drivers GREEN must satisfy. Measure again post-GREEN with `go test -cover -run 'TestCloseGate|TestCloseDep|TestQuarantineClose|TestVerifiedClose_HardGate' ./internal/core/`.
- No implementation written (RED discipline respected).

## Open edges for the next phase
- GREEN implements `runCloseHardGates` in internal/core/bead_closer.go + wires it into `verifyBeforeClose` before the judge round. Command execution should go through the FR-003 sandboxed runner (separate bead); until then a plain exec in worktreePath satisfies these tests.
- Gate-3 chain-member detection contract: title prefix RED/GREEN/REFACTOR/SYNTAX. Approval-gate and operator-closed beads keep legitimate open-children closes via the singleton warn-only branch.
- Fail-open legacy path = no `**Verification Command:**` block; still must emit `verification_command_missing`.
