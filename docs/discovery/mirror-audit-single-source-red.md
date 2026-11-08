# RED handoff — Shared audit package (single source of truth)

## What changed (files + one line each)
- internal/mirroraudit/single_source_test.go (new, deliberately UNCOMMITTED): 3 tests pinning the single-source contract — TestSingleSource_ToolImportsPackage (tool must `"github.com/haulmatic-dev/hydra/internal/mirroraudit"`), TestSingleSource_ToolHasNoDuplicateClassifier (no `parseJSONLLine(` nor `func classify`/`func audit` in tools/beads-mirror-audit/main.go), TestSingleSource_HistoryDocumented (CHANGELOG records the single-source contract). Untracked until GREEN implements so the shared gate stays green for sibling beads.
- CHANGELOG.md: Documentation entry under Unreleased pointing at this handoff + the uncommitted RED pin.
- No production code change (RED discipline; implementation is GREEN's obligation).

## Verification (commands run + pass/fail)
- ENFORCE/RED run: `go test ./internal/mirroraudit/ -count=1` — FAIL, exactly the contract:
  - `TestSingleSource_ToolImportsPackage: tools/beads-mirror-audit/main.go does not import internal/mirroraudit — classification is duplicated, not shared`
  - `TestSingleSource_ToolHasNoDuplicateClassifier: tools/beads-mirror-audit/main.go still defines "parseJSONLLine(" — duplicate of the shared classifier`
  FAIL (expected, right reason: tool duplicates the shared classifier today).
- Full fixture suite minus RED pins: `go test ./internal/mirroraudit/ -count=1` on the adopted subset — PASS (~85.2% pkg stmts), incl. TestAudit_CleanFixture.
- Coverage: `go tool cover -func` (audit.go) — Drift 100%, Audit 97.3%, add 100%, parseJSONLLine 100%; BrListJSON 0.0% (exec seam, exercised only by tool-level tests); package total 91.2%.
- Tool behavior contract stays green in parallel: `go test ./tools/beads-mirror-audit/ -count=1` — PASS (ok 2.187s).
- `go build ./tools/beads-mirror-audit/` — PASS (RC=0).
- AC3 `go build ./...` — FAILS, but only due to the foreign untracked `internal/core/pan_mirror_health.go` (parallel bead: `o.brUpdater`/`o.emitEvent` undefined on *Overseer). Not this strand's scope; flag for the owning bead.

## Contract GREEN must implement
1. Update tools/beads-mirror-audit/main.go to import `internal/mirroraudit` and source classification (Drift) from it; delete the local parseJSONLLine/classify duplicates (BrListJSON exec seam may remain tool-side).
2. Evidence in GREEN: same two tests flip PASS, `go test ./internal/mirroraudit/ -count=1` fully green, `go test ./tools/beads-mirror-audit/ -count=1` still green, `go build ./...` exits 0.

## Open edges for the next phase
- Commits the test file once GREEN lands the tool change (single commit keeps the intermediate gate history clean).
- pan_mirror_health.go build break must be resolved by the mirror-health bead before any fleet-wide `go build ./...` gate can pass.
- Stray untracked `beads-mirror-audit/` dir at worktree root (unrelated artifact from an earlier session; safe to delete after confirming no tool churn inside).
