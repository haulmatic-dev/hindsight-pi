# Discovery: Reconciler integrate runs the suite gate (FR-003, bead hydra-z9ejs)

Chain: suite-gate in `IntegrateBranch` (RED bead hydra-3uxno → this discovery → GREEN).
RED contract: `internal/core/worktree_suite_gate_test.go` (gate `SUITE_GATE_MOVE_ENFORCE=1`),
sentinel `internal/core/worktree_suite_gate.go` (`ErrQuarantineSuiteGateBlocked`).

## Problem

The full-suite gate lived in `IntegrateBranchAndRemove` on the *chain* worktree
(`m.worktreePath(projectPath, key)`). Two integration routes bypass it:

1. **Quarantine reconciler branch-only path** — `QuarantineReconciler.Reconcile` calls
   `worktreeMgr.IntegrateBranch(projectPath, key, branch)` directly
   (internal/core/quarantine_reconciler.go:286) after `QuarantineBranchAndRemove`
   has already removed the chain worktree; no worktree ⇒ no gate ⇒ broken chains
   can be merged back to the checkout unverified.
2. **Worktree reconciler** — `ReconcileOrphanedWorktrees` integrates stale
   `hydra/wt-*` branches via `IntegrateBranchAndRemove` (worktree_reconciler.go:85);
   worktree exists there so it is gated today, but the gate runs pre-merge on the
   chain worktree, not on the merged result.
3. **Singleton close fallback** — `Integrate` delegates to `IntegrateBranch`
   (worktree_manager.go:769); close-path integrates go ungated.

## Resolved design (GREEN contract)

- **Single gate site**: inside `IntegrateBranch`, after the scratch merge
  resolves (post the `merged:` label, before any ref-update/fast-forward of the
  checkout branch). It runs on the always-present
  `integrate-scratch-<key>` worktree, so the merged result — the only tree that
  was never verified — is what the suite actually sees. This makes the
  branch-only path gated with zero extra branches (the scratch worktree is
  created from HEAD regardless of whether a chain worktree exists).
- **Moved, not duplicated**: the pre-merge block is deleted from
  `IntegrateBranchAndRemove`, which becomes a pure forward to `IntegrateBranch`
  + teardown. Exactly ONE full-suite run per clean integrate (test 3 pins
  `gateRuns == 1`).
- **Typed error, no string sniffing**: gate failure returns
  `fmt.Errorf("chain suite gate: %v: %w", gerr, ErrQuarantineSuiteGateBlocked)`.
  Callers classify with `errors.Is`.
- **Fail-closed on the checkout**: a gate failure returns before the
  ref-update/fast-forward step, so the checkout branch tip is untouched
  (tests 1 and 2 pin `HEAD` unchanged); the scratch worktree is torn down by
  the existing `defer`.
- **Nil gate fails open** (test 4): `m.SuiteGate == nil && defaultWorktreeSuiteGate == nil`
  ⇒ integrate proceeds ungated (standalone/legacy daemons keep working).
- **AC2 event wiring**:
  - `QuarantineReconciler.Reconcile` re-integrate site: on
    `errors.Is(err, ErrQuarantineSuiteGateBlocked)` keep the quarantine branch
    AND the registry entry (do not classify as a merge conflict, do not attempt
    merge-tree triage), increment `IntegrateAttempts` for bounded retries
    (`maxAttempts` fails closed to human review), emit exactly ONE
    `quarantine_suite_gate_blocked` event (`Bead = chain key`, fields
    `branch`, verbatim gate output in `Detail`) and SUPPRESS the
    `chain_reintegrate_failed` emission for this class so the event contract
    stays "exactly one".
  - `ReconcileOrphanedWorktrees` stale-branch site: same typed-error match —
    keep the branch (do NOT quarantine-branch-and-remove, which would consume
    the branch while the post-merge tree is broken), emit the event, `continue`.
- **Singleton close fallback**: `Integrate` inherits the gate for free; its
  existing error handling (failIntegration/integration-failed) needs no change.

## Observations at discovery time (live worktree, 2026-11-08)

- The GREEN gate-move edit is present in the worktree as an uncommitted
  modification to `internal/core/worktree_manager.go` (a concurrent strand is
  landing it); the pre-merge block in `IntegrateBranchAndRemove` is removed and
  the post-merge gate block sits after `merged:`.
- `ReconcileOrphanedWorktrees` already carries the typed-error branch emitting
  `quarantine_suite_gate_blocked` (uncommitted modification).
- `QuarantineReconciler.Reconcile` does NOT yet handle
  `ErrQuarantineSuiteGateBlocked` — **open edge for GREEN** (AC2 test 5).
- Cross-strand compile hazard: `containsLabel` is declared twice in test files
  (`bead_closer_fingerprint_test.go` and `quarantine_unverifiable_test.go`,
  sibling unverifiable-scar chain) — `go test ./internal/core/` build-fails in
  the shared tree until one is renamed; recorded, out of scope here.
- A sibling strand's `quarantine_unverifiable.go` RED seam leaves
  `*QuarantineReconciler` fields referencing `RevalidateVerdict` uncommitted;
  transient build breakage in this worktree is strand-local, not from this
  chain.

## Verification evidence (clean-window run)

Run with the sibling strand's in-flight `quarantine_unverifiable*` files set
aside and `quarantine_reconciler.go`/`worktree_reconciler.go` at HEAD:

- `SUITE_GATE_MOVE_ENFORCE=1 go test ./internal/core/ -run 'TestSuiteGate' -count=1`
  → `ok ... 0.849s` (gate-move tests 1–4 green against the concurrent GREEN edit).
- Default run → `ok ... 0.012s` (skip-loud gate healthy, daemon gate green).
- Test 5 (reconciler typed event) enforcement result NOT VERIFIED in that window
  (verbose run was blocked by the sibling build break); treat AC2 wiring as
  GREEN's remaining obligation.

## AC mapping

| AC | Status | Where |
|---|---|---|
| Gate on `integrate-scratch-<key>` post-merge inside `IntegrateBranch`, moved not duplicated; reconciler + singleton fallback both gated | Design resolved; GREEN edit observed in-flight | `worktree_manager.go` IntegrateBranch |
| Quarantine kept + typed `quarantine_suite_gate_blocked` via `errors.Is` | Design resolved; `QuarantineReconciler` wiring still open | `quarantine_reconciler.go` |
| Nil gate fails open; exactly ONE suite run per clean integrate | Pinned by tests 3–4 | `worktree_suite_gate_test.go` |
