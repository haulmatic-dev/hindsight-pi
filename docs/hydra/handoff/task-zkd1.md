# refactor handoff (task-zkd1, bead hydra-8vds)

## What changed (files + one line each)
- internal/core/build_dependency_graph.go: `DAG.RollbackReleases` — inline released-predicate extracted into named `releasedByCompletion(node *Node) bool` (InDegree==0 && StatusOpen, doc comment explains the idempotence argument); dropped the pre-allocated `make([]string, 0)` in favor of `var rolledBack []string` with a direct return (nil slice when nothing re-blocked, same contract).
- internal/core/worker_lifecycle.go: `RollbackReleasedDependents` — event emission extracted into nil-sink-safe `emitReleaseRolledBack`; event type and reason lifted to named constants `releaseRolledBackEventType` ("release_rolled_back") and `rollbackReasonCloseRejected` ("close_rejected"). Fail-open log path unchanged.
- internal/core/dag_rollback_test.go: duplicated list-scan loops in `dagRollbackIsReady`/`dagRollbackIsBlocked` deduped into one `dagRollbackBeadInList` helper; assertions untouched.
- CHANGELOG.md: Unreleased/Changed entry for the refactor.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit baseline): `go test ./internal/core/ -run 'TestDagRollback_' -count=1 -v` — PASS, 5/5 `--- PASS` (CompletionReleasesDependent, RejectedCloseRollsBackReleasedDependent, RejectNoReleasedDependentsEmitsNoEvent, RollbackIdempotent, VerifiedCloseKeepsDependentsReleased), ok 0.016s.
- AFTER: same command — PASS, 5/5, ok 0.014s. Behavior identical.
- `go vet ./internal/core/` — clean (VET_OK).
- `gofmt -l` on the three touched files — empty (clean).
- `go build ./internal/core/` — BUILD_OK.
- Scoped DAG regression: `go test ./internal/core/ -run 'TestDag|TestCampaignPark' -count=1` — PASS.
- Benchmarks: none exist for the DAG rollback path (`grep -rn "func Benchmark"` over dag.go/dag_builder.go/build_dependency_graph.go/worker_lifecycle.go/dag_rollback_test.go → 0 hits); perf criterion N/A.
- Discovery: `cm context` queried (no matching anti-pattern); `graphify query` confirmed target symbols (RollbackReleases @ build_dependency_graph.go:269/284, RollbackReleasedDependents @ worker_lifecycle.go:424). hindsight_search — not connected (skip rule applied).

## Open edges for the next phase
- No real bug found; nothing to record via `cm add`. `isDependencyError` (worker_lifecycle.go, adjacent) is a loose substring match, out of this bead's scope.
- RED-phase gate seam `dagRollbackImplemented = true` retained (contract tests are ungated, consistent with landed GREEN).
