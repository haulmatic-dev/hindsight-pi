# discovery handoff (hydra-z9ejs — FR-003 reconciler integrate runs the suite gate)

## What changed (files + one line each)
- docs/discovery/suite-gate-integrate-move-z9ejs.md: discovery doc — gate run resolved to a single site inside `IntegrateBranch`, post the `merged:` label, on the always-present `integrate-scratch-<key>` tree (branch-only reconciler path and singleton fallback gated with zero extra branches); pre-merge block deleted from `IntegrateBranchAndRemove` (moved, not duplicated, exactly ONE run per integrate); typed `ErrQuarantineSuiteGateBlocked` wrap; nil gate fails open; AC2 event wiring plan for `QuarantineReconciler.Reconcile` (errors.Is, keep branch+entry, bounded attempts, one `quarantine_suite_gate_blocked` event, `chain_reintegrate_failed` suppressed for this class) and `ReconcileOrphanedWorktrees`.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verification (commands run + pass/fail)
- `SUITE_GATE_MOVE_ENFORCE=1 go test ./internal/core/ -run 'TestSuiteGate' -count=1` — PASS (`ok ... 0.849s`) in a clean window (sibling strand's in-flight `quarantine_unverifiable*` files set aside, `quarantine_reconciler.go`/`worktree_reconciler.go` at HEAD): the concurrent GREEN edit to `worktree_manager.go` satisfies tests 1–4 (scratch post-merge gate, branch-only gated, exactly one run, nil fail-open).
- Default (no enforce) same run — PASS (`ok 0.012s`), daemon gate green.
- AC2 (QuarantineReconciler typed event, test 5) enforcement NOT VERIFIED — the shared worktree concurrently mutated by sibling strands; `QuarantineReconciler.Reconcile` has no `ErrQuarantineSuiteGateBlocked` handling at inspection time (rg: 0 hits outside tests/stub).
- Commit f244dde verified via `git show --stat` (CHANGELOG.md +1, discovery doc +99).
- hindsight_search at bead start — not connected (skip rule applied). graphify-out/graph.json absent in cwd → rg/handoff fallback per protocol.

## Open edges for the next phase (GREEN obligations)
1. Wire `QuarantineReconciler.Reconcile`: `errors.Is(err, ErrQuarantineSuiteGateBlocked)` → keep quarantine branch + registry entry, increment IntegrateAttempts (bounded by maxAttempts), emit exactly ONE `quarantine_suite_gate_blocked` (Bead=chain key, fields branch + verbatim gate output), suppress `chain_reintegrate_failed` for this class. Pinned by TestSuiteGate_ReconcilerPathKeepsQuarantineAndEmitsTypedEvent.
2. Keep the (concurrent strand's) `ReconcileOrphanedWorktrees` typed-error branch committed — it currently exists only as an uncommitted change in this shared worktree.
3. Cross-strand build hazard recorded: `containsLabel` declared in both `bead_closer_fingerprint_test.go` and `quarantine_unverifiable_test.go` — one must be renamed or `go test ./internal/core/` stays build-broken.

HYDRA_VERDICT: {"done": true, "ac_met": ["discovery deliverables: contract decisions doc + changelog + handoff", "GREEN design resolved + observably landing (tests 1-4 enforce-pass in clean window)"], "ac_unmet": [], "files_changed": ["docs/discovery/suite-gate-integrate-move-z9ejs.md", "CHANGELOG.md", "docs/hydra/handoff/task-z9ejs.md"], "verification": [{"cmd": "SUITE_GATE_MOVE_ENFORCE=1 go test ./internal/core/ -run 'TestSuiteGate' -count=1", "result": "PASS (ok 0.849s, clean window)"}, {"cmd": "go test ./internal/core/ -run 'TestSuiteGate' -count=1", "result": "PASS (ok 0.012s)"}]}
