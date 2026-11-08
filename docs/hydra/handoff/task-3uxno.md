# red handoff

## What changed (files + one line each)
- internal/core/worktree_suite_gate_test.go (new): 5 enforce-gated contract tests (`suiteGateMoveRequireImpl` / `SUITE_GATE_MOVE_ENFORCE=1`, established codebase RED pattern, skip loudly by default) pinning all parent ACs — (1) gate runs inside IntegrateBranch on the `integrate-scratch-<key>` worktree AFTER the merge (merged content visible in the gate's worktreePath, path suffix asserted) and a gate failure wraps the typed `ErrQuarantineSuiteGateBlocked` with the checkout branch tip untouched; (2) branch-only path (no hydra/wt-<key> worktree, branch only — the reconciler's shape and also the singleton close fallback via Integrate→IntegrateBranch) is gated and fail-closed; (3) exactly ONE gate run per clean IntegrateBranchAndRemove integrate, and the run lands on the integrate-scratch tree; (4) nil gate (m.SuiteGate=nil + defaultWorktreeSuiteGate=nil) fails open and lands the work; (5) quarantine reconciler on `errors.Is(err, ErrQuarantineSuiteGateBlocked)` keeps the quarantine branch + entry and emits exactly one typed `quarantine_suite_gate_blocked` event naming the chain key.
- internal/core/worktree_suite_gate.go (new): inert RED stub — only `var ErrQuarantineSuiteGateBlocked = errors.New("quarantine suite gate blocked")` so the typed-error contract compiles and RED fails for the missing feature, not a build error. Zero behavior.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestSuiteGate_' -count=1` — PASS (ok 0.007s; all contract tests skip loudly pre-GREEN, daemon lint gate stays green).
- `SUITE_GATE_MOVE_ENFORCE=1 go test ./internal/core/ -run 'TestSuiteGate_' -count=1 -v` — FAIL, 4 of 5 tests RED for the RIGHT reason (quoted failures): "expected gate failure, got nil error" / "branch-only integrate must be gated, got nil error" / "gate must run on the integrate-scratch worktree" (ExactlyOneRun) / "gate-blocked chain: quarantine branch must survive". TestSuiteGate_NilGateFailsOpen passes pre-GREEN (behavior-preserved regression guard).
- `gofmt -l internal/core/worktree_suite_gate_test.go internal/core/worktree_suite_gate.go` — clean.
- `go vet ./internal/core/` — clean.
- RED gate coverage measurement: contract at behavioral level (5 tests); grep-based coverage N/A pre-GREEN — GREEN must run `-run 'TestSuiteGate_' -coverprofile` and report coverage for the moved gate block.
- hindsight_search at bead start — ran (20 results: FR-001(c)/FR-003 partial-spec observation, scratch-worktree integrate lineage, `chain suite gate` in IntegrateBranchAndRemove).

## Contract GREEN must implement
- Move (not duplicate) the gate block from `IntegrateBranchAndRemove` into `IntegrateBranch`, running on `m.worktreePath(projectPath, "integrate-scratch-"+key)` after the merge verification (`isAncestorOf` check passes, before checkout fast-forward); gate failure wraps `ErrQuarantineSuiteGateBlocked` and errors out before landing on the checkout branch; `IntegrateBranchAndRemove` keeps only the forward to IntegrateBranch.
- quarantine_reconciler.go re-integrate path: on `errors.Is(err, ErrQuarantineSuiteGateBlocked)` emit `quarantine_suite_gate_blocked` EventRepo with the chain key (`Bead: e.Key`, Fields runnable: branch/attempts) and return WITHOUT incrementing IntegrateAttempts into retry churn beyond the keep-quarantine contract (entry stays, branch stays).

## Open edges for the next phase
- `gofmt`/vet clean; no production code beyond the inert sentinel stub (hard RED rule respected).
- GREEN should delete the old gate block in IntegrateBranchAndRemove (single run, no duplication) and re-run the existing TestIntegrateBranchAndRemoveGated family — those tests assert the "chain suite gate" string prefix, which must keep appearing alongside the typed sentinel (GREEN: keep `chain suite gate: %w` wrapping around the sentinel wrap) or be updated in the same edit budget if the wording changes.
