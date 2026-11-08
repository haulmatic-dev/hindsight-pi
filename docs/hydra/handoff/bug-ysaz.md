# red handoff (bug-ysaz)

## What changed (files + one line each)
- None. Verify-first close: the findProjectPathForBead fix for user-namespaced bead IDs already exists on this integration base and passes its regression tests.

## Verification (commands run + pass/fail)
- Reproduction attempt (A1): the hypothesized defect is absent on this base — `findProjectPathForBead` (internal/core/overseer.go:1365) now resolves project via the DAG bead record (`node.Bead.Project`) FIRST, then falls back to the exact-segment fallback (strip trailing `-<suffix>`, longest-name match, `ns == project.Name || strings.HasSuffix(ns, "-"+project.Name)`) — no substring matching, so "web" cannot match "webapp".
- Regression tests present and green: `internal/core/epic_close_guard_test.go:203` TestFindProjectPathForBead_NamespacedID ("ajantha-srv-smart-engine-1u3" resolves via DAG record + persists close) and :231 TestFindProjectPathForBead_NamespacedIDSegmentFallback (ghost-node/no-DAG path resolves via exact-segment; "engine" does not steal from "srv-smart-engine").
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestPersistClosedEpics|TestFindProjectPathForBead|TestEpicCloseGuard|TestRepoDirForBead|TestMultiRepoPaths' -count=1` — PASS (ok, 10 tests incl. persist-path, namespaced-DAG, segment-fallback, multi-repo repoDirForBead).
- `git status --short` — clean; no work to commit.

## Open edges for the next phase
- ADJACENT DEFECT (named in bead, still open): `epicHasOpenDependent` is vacuous — `ParseBeadListPaginated` does not populate `Bead.Dependencies` (only `dependency_count` metadata), so the fail-closed store guard never sees dependents; gate is fail-open in practice. Separate bead.
- NEXT failure layer: `br close` as hydra user on a user-owned (e.g. ajantha-owned) store may hit the store-ownership tripwire (exit 128); verify a live epic close lands in-store before claiming production-fixed.
