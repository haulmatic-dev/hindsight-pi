# red handoff

- **Bead:** hydra-63bh — RED: Write tests for Dependent-release rollback on rejected close
- **Phase:** red
- **Bead ID:** 63bh
- **Date:** 2026-07-31 (dogfood worktree `hydra-8vds`)

## What changed

RED-phase tests for dependent-release rollback on rejected close. No feature
implementation landed in this bead — tests pin behavior only.

**Files changed:**

- `internal/core/dag_rollback_test.go` (new) — five tests under the
  `TestDagRollback_` selector:
  1. `TestDagRollback_CompletionReleasesDependent` — UNGATED baseline: worker
     completion via `MarkCompleted` marks the bead completed
     (`g.dag.GetNodeStatus`) and returns the newly-released dependent.
  2. `TestDagRollback_RejectedCloseRollsBackReleasedDependent` — after B
     completes and D is released, a rejected close must roll D back to blocked
     and emit exactly one `release_rolled_back` event to the central sink.
  3. `TestDagRollback_RejectNoReleasedDependentsEmitsNoEvent` — rejecting a
     bead with no released dependents emits zero events.
  4. `TestDagRollback_RollbackIdempotent` — a second rejection after rollback
     emits nothing new and returns no additional re-blocked beads.
  5. `TestDagRollback_VerifiedCloseKeepsDependentsReleased` — a bead that
     closes verified (not rejected) leaves dependents released/blocked-by-
     nothing; no rollback events.
- `internal/core/worker_lifecycle.go` — inert stub
  `RollbackReleasedDependents(g *DependencyGraph, sink *EventSink, project,
  beadID string) []string` returning nil, gated by
  `const dagRollbackImplemented = false`.
- `internal/core/build_dependency_graph.go` — inert stub
  `(*DependencyGraph) RollbackReleases(beadID string) ([]string, error)`
  returning `nil, nil`, appended after the `handleEdgeError` helper.
- `CHANGELOG.md` — RED entry under `## [Unreleased]` / `### Added`.

**Test infrastructure conventions (repo pattern):**

- `dagRollbackRequireImpl(t)` reads `dagRollbackImplemented`; when false it
  `t.Skip`s loudly with a pointer to this handoff, so the default suite exits 0
  (daemon gate stays green pre-GREEN). When `DAG_ROLLBACK_ENFORCE=1` is set, a
  missing implementation is `t.Fatal` — this prevents negative-contract tests
  (2/3/4: "no event emitted") from passing trivially against the inert stub.
- Events are asserted by reading the central JSONL sink:
  `ReadEvents(filepath.Join(sink.centralDir, time.Now().UTC().Format("2006-01-02")+".jsonl"))`,
  counting `Type == "release_rolled_back"`; a missing file counts as 0
  (precedent: `readEventsOfType` in `bead_store_guard_test.go`).
- Graph fixture: `BuildFromBeads` over `bead-b` (open) with dependent `bead-d`
  (open, `Dependencies: []types.BeadID{"bead-b"}`). Node status checked via
  `g.dag.GetNodeStatus` (not exposed on the `DependencyGraph` wrapper).

## Verification

Default mode (daemon gate view):

```
$ go test ./internal/core/ -run 'TestDagRollback_' -count=1 -v
=== RUN   TestDagRollback_CompletionReleasesDependent
--- PASS: TestDagRollback_CompletionReleasesDependent (0.00s)
=== RUN   TestDagRollback_RejectedCloseRollsBackReleasedDependent
    dag_rollback_test.go:XX: SKIPPED (RED): DAG_ROLLBACK feature not implemented yet ...
--- SKIP: TestDagRollback_RejectedCloseRollsBackReleasedDependent
... (3 more skips)
ok
```

1 PASS + 4 loud skips, suite `ok`, no `[no tests to run]` — satisfies parent
AC 5 while the feature is unimplemented.

RED proof (true-fail mode):

```
$ DAG_ROLLBACK_ENFORCE=1 go test ./internal/core/ -run 'TestDagRollback_' -count=1 -v
--- PASS: TestDagRollback_CompletionReleasesDependent
--- FAIL: TestDagRollback_RejectedCloseRollsBackReleasedDependent
    dag_rollback_test.go:129: rolledBack = [], want [bead-d]
--- FAIL: TestDagRollback_RollbackIdempotent
    dag_rollback_test.go:191: first rolledBack = [], want [bead-d]
--- FAIL: (no-event tests via dagRollbackRequireImpl t.Fatal)
```

4 FAIL for the RIGHT reason (feature missing, not compile error), 1 PASS.

Coverage (scoped run, stubs only): `RollbackReleases` 0.0%,
`RollbackReleasedDependents` 0.0% — expected pre-GREEN; GREEN bead must re-run
with `-coverprofile` and report real numbers for the new implementations.

`go vet ./internal/core/` clean on the touched package.

## Open edges — contract for the GREEN bead

GREEN must implement, in `worker_lifecycle.go` (flip
`dagRollbackImplemented = true`) and `build_dependency_graph.go`:

1. `RollbackReleases(beadID)` — re-block every dependent released by
   `beadID`'s completion: for each dependent D that is currently open only
   because `beadID` completed, restore its blocked-by state so
   `g.dag.GetNodeStatus(D)` returns the blocked status again. Must be
   idempotent (second call returns `nil` re-blocked set, no state change).
2. `RollbackReleasedDependents(g, sink, project, beadID)` — orchestrates:
   calls `RollbackReleases`, and when ≥1 dependent was re-blocked emits
   **exactly one** `Event{Type: "release_rolled_back", Project, Bead: beadID}`
   via `sink.Emit`/`EmitRepo`. When zero dependents were re-blocked, emits
   **nothing**. Idempotent by the same rule.
3. Wire into the rejected-close path (hard-gate rejection / requeue flow in
   `bead_closer_hard_gates.go` / shepherd close handling): on close
   **rejection**, roll back releases granted by that bead's completion; on
   **verified** close, do nothing (dependents stay released).
4. Do NOT touch `MarkCompleted` release semantics — test 1 pins them as the
   baseline and is UNGATED.

Edge notes:

- `BuildFromBeads` prunes edges to terminal deps, so a re-opened/re-blocked
  dependent's edge restoration must happen at the DAG level
  (`g.dag`), not by rebuilding from beads.
- The typed event name is pinned as `release_rolled_back` (style match:
  `epic_close_refused`, `close_dep_open`). Do not rename without updating the
  test helper `dagRollbackEventCount`.
- Guard the close-rejection call site so rollback runs once per rejection
  (idempotency contract), not once per dependent.
