# RED handoff

## What changed (files + one line each)
- internal/mirroraudit/single_source_test.go (new): two enforce-gated tests pinning the single-source-of-truth contract — `TestSingleSource_ToolImportsPackage` requires tools/beads-mirror-audit to import internal/mirroraudit; `TestSingleSource_ToolHasNoDuplicateClassifier` requires no `parseJSONLLine(` duplicate; gate via `MIRROR_AUDIT_SOT_ENFORCE=1` so the tool still runs standalone (table-driven, t.Parallel).
- CHANGELOG.md: Added entry under Unreleased (mirror-health sweep through shared package + single-source regression test).

Note: internal/core/pan_mirror_health.go + its test are a parallel GREEN bead's files in this worktree; not touched here. Its import `logx` currently breaks `go build ./...` — out of this bead's scope.

## Verification (commands run + pass/fail)
- `MIRROR_AUDIT_SOT_ENFORCE=1 go test ./internal/mirraudit/ -count=1 -run TestSingleSource` — FAIL for the RIGHT reason: both failures quote the missing feature (tool does not import shared package / duplicates parseJSONLLine). RED confirmed.
  Exact failures:
    single_source_test.go:49: tools/beads-mirror-audit/main.go does not import internal/mirroraudit — classification is duplicated, not shared
    single_source_test.go:70: tools/beads-mirror-audit/main.go still defines "parseJSONLLine(" — duplicate of the shared classifier; classification must come from mirroraudit only
- `go test ./internal/mirroraudit/ -count=1 -cover` — PASS (85.2% stmt coverage, package suite green with fixture cases incl. TestAudit_CleanFixture).
- `go build ./...` — FAIL, but for an unrelated reason: internal/core/pan_mirror_health.go:5:2 "package logx is not in std" (parallel bead's file, not this bead's scope).

## Open edges for the next phase (GREEN)
- Implement the single-source-of-truth refactor: tools/beads-mirror-audit/main.go must import internal/mirroraudit (use its classifier/exported API) and delete its local parseJSONLLine duplicate.
- After refactor, `MIRROR_AUDIT_SOT_ENFORCE=1 go test ./internal/mirroraudit/ -count=1` must be GREEN.
- Watch for build-blocker from internal/core/pan_mirror_health.go (separate bead) when running `go build ./...` — coordinate, do not edit that file here.
