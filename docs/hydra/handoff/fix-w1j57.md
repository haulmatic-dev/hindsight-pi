# fix handoff (fix-w1j57, original hydra-ogm5)

## What changed (files + one line each)
- None (source). Verify-first close: the shared-resolver adoption in the close path (sibling-test scoping) is already implemented and committed on this integration base. Original bead hydra-ogm5's exit-1 was pure worker infra — `❌ Pi rpc turn reached agent_end but the model call FAILED: Credential store read failed for llm-router: EACCES: permission denied, open '/opt/dev-tools/config/pi/auth.json'` — no lint/test finding was ever produced, and the GREEN strand (green-8vds) had already landed the work before the close attempt failed.
- docs/hydra/handoff/fix-w1j57.md: this handoff (only file added).

## Verified state
- `internal/core/beadfiles_adoption.go` (committed, 03bfa8dd lineage): `beadTargetFiles` delegates to the shared `internal/beadfiles.Resolve` and applies one-directional `siblingTestExpansion` (every resolved impl `x.go` gains `x_test.go`; test-only declarations gain nothing).
- `internal/core/bead_closer.go:1368` consumes the adopted `beadTargetFiles`; the bead-local `beadTargetFiles`/`splitFileList` parser is gone — `grep -rn 'splitFileList' internal/core/ --include='*.go'` → 0 hits (single implementation lives in `internal/beadfiles/resolve.go`).
- 9 production call sites on the shared resolver: prespawn_probe.go:43, gate_close.go:75, bead_closer.go:1368, satisfied_close.go:93, fix_audit.go:138, target_overlap.go:75+86, integration_retest.go:678, beadfiles_adoption.go:23 (definition).
- Regression coverage: `TestBeadfilesAdoption_ScopeGate_SiblingTestInScope`, `_TargetOverlap_SiblingTestConflicts`, `_SatisfiedClose_CleanScopeCloses`, `_FixAudit_CleanScopeVerifies`, `_IntegrationRetest_CleanScopePasses`, `_GateClose_CleanScopePasses`, `_PrespawnProbe_CleanScopeProbes`, `_SiblingTestExpansion`, `_ResolveAdopted` + `TestBeadTargetFiles` — all pin the behavior-changed callers.
- CHANGELOG.md already carries the Changed entry under Unreleased (landed with the green-8vds chain).

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestBeadTargetFiles|TestBeadfilesAdoption|TestBeadfiles' -count=1 -v` — PASS, 10/10 `--- PASS`, `ok github.com/haulmatic-dev/hydra/internal/core 0.052s`.
- `go build ./internal/core/` — PASS (BUILD_OK).
- `go vet ./internal/core/` — PASS (VET_OK, clean).
- `git status --short` — clean tree (only this handoff added).
- `git log --oneline -- internal/core/beadfiles_adoption.go` — 03bfa8dd (committed at HEAD lineage, not worktree-local).
- Discovery: CASS queried (`cm context`) — no matching anti-pattern (unrelated bv-client rule only); `hindsight_search` unavailable (skip rule); graphify-out/graph.json present but verify-first, no new code.
- Duplicate-mechanism grep (A3): no second parser exists — `splitFileList` defined only in `internal/beadfiles/resolve.go`.

## Open edges for the next phase
- None for this gap. The adoption is the superset already described in green-8vds; no further close-path scoping work pending.

HYDRA_VERDICT: {"done": true, "ac_met": ["verify-first: shared-resolver adoption landed, sibling-test scoping tests green, duplicate parser removed"], "ac_unmet": [], "files_changed": ["docs/hydra/handoff/fix-w1j57.md"], "verification": [{"cmd": "go test ./internal/core/ -run 'TestBeadTargetFiles|TestBeadfilesAdoption|TestBeadfiles' -count=1 -v", "result": "PASS 10/10"}, {"cmd": "go build ./internal/core/", "result": "PASS"}, {"cmd": "go vet ./internal/core/", "result": "PASS"}]}
