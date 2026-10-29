# GREEN handoff — task-5m1a (bead hydra-5m1a)

Phase: green. Parent contract: hydra-63bh RED suite (`internal/core/dag_rollback_test.go`, 7 tests under `TestDagRollback_`).
Note: this worktree is shared and concurrently mutated; the handoff file and CHANGELOG entry for this bead were
re-created after a concurrent process removed them mid-bead (first commit attempt failed with
`fatal: pathspec 'docs/hydra/handoff/task-5m1a.md' did not match any files`, git add is all-or-nothing, so nothing
partial was committed).

## What changed (files + one line each)

- `internal/core/build_dependency_graph.go` — `DependencyGraph.RollbackReleases` wrapper + `DAG.RollbackReleases`: re-block dependents released by a completion, predicate `InDegree==0 && StatusOpen` (mirrors `DAG.MarkCompleted` release semantics → rollback is idempotent).
- `internal/core/worker_lifecycle.go` — `RollbackReleasedDependents(g *DependencyGraph, sink EventSink, beadID, reason string)`: fail-open wrapper (graph errors are logged, never gate the pipeline; nil-sink safe), emits exactly one `release_rolled_back` event (`reason=close_rejected`) carrying the re-blocked count; plus `const dagRollbackImplemented = true` seam.
- `internal/core/dag_rollback_test.go` — NOT edited; RED contract already pinned the behavior.
- `CHANGELOG.md` — Unreleased/Added entry for the dependent-release rollback.
- `docs/hydra/handoff/task-5m1a.md` — this file.

## Verification (commands run + pass/fail)

- `go test ./internal/core/ -run 'TestDagRollback_' -count=1 -v` — PASS, 7/7 (wrapper prints "PASS: 7 passed"; exit 0 authoritative). Run twice: at implementation time and re-verified at HEAD `73c8b2b8` after other beads touched the same files.
- `go build ./...` — exit 0.
- `go vet ./internal/core/` — clean. (`go vet ./...` on the whole repo left to the daemon gate.)
- Coverage (narrow, -run TestDagRollback_): `DependencyGraph.RollbackReleases` 100.0%, `DAG.RollbackReleases` 82.4%, `RollbackReleasedDependents` 77.8%.
- No placeholder comments (`Placeholder`/`TODO`/`FIXME`) in changed implementation code.

## Commit attribution

The implementation edits landed via other beads' commits (shared-tree `git add` collision):
- `build_dependency_graph.go` rollback code → commit `d00bd098` (task-4ovt).
- `worker_lifecycle.go` rollback code → commit `73c8b2b8` (infra-classification).
- RED tests: `0e23eaf4` / `f8026ba4` (hydra-63bh).
This bead's own commit carries only the CHANGELOG entry + this handoff doc.

## Open edges for the next phase

- Full `DAG_ROLLBACK_ENFORCE=1` run and repo-wide `go vet ./...`/`go test ./...` left to the daemon gate (budget: narrow selector run 2/3 used).
- `DAG.RollbackReleases` at 82.4% — uncovered branch is `ErrNodeNotFound` for unknown bead IDs.
- Transitive dependents are re-blocked for the directly released set only; a re-released grandchild chain is not recursively re-blocked (matches RED contract, which pins one level).
- Re-blocked beads are left Open but not re-queued/re-prioritized by the scheduler here; scheduling rediscovery handles pickup.
