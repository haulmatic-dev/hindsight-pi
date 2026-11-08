# refactor handoff (task-71crk)

## What changed (files + one line each)
- internal/core/worktree_manager.go: extracted named `suiteGate()` helper (per-instance SuiteGate > package default, nil both) from the inline gate-resolution block in `IntegrateBranch`; gate call site now `if gate := m.suiteGate(); gate != nil`. Behavior identical — gate still runs once per integrate on the integrate-scratch post-merge tree; typed error wrap, nil fail-open, `errors.Is` handling in both reconcilers unchanged (all parent ACs already landed by GREEN 5jcb3, commit d5cbf9c6).
- CHANGELOG.md: Unreleased/Changed entry.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit): `go test ./internal/core/ -run 'TestSuiteGate' -count=1` — ok (0.911s), 5/5 PASS incl. ScratchAfterMerge/BranchOnlyPathGated/ExactlyOneRun/NilGateFailsOpen/ReconcilerTypedEvent.
- AFTER: same run plus `TestWorktree` — ok (2.936s), 0 FAIL. `gofmt -l` clean. `go vet ./internal/core/` clean.
- No benchmarks exist for the integration path; Intermediate one-call refactor has no perf effect.

## Open edges for the next phase
- None for this chain. Operator escape hatch (quarantine removal integrates via ungated Integrate) remains pre-existing documented behavior.
