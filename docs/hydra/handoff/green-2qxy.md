# green handoff (Shared audit package — single source of truth)

## What changed (files + one line each)
- Verify-first close: the FR-1 GREEN implementation (internal/mirroraudit shared package + tools/beads-mirror-audit thin wrapper + single_source_test.go) was already landed on this branch by prior strands (audit package, parser envelope/`--all` fixes, mirror-health sweep consumption).
- internal/mirroraudit/audit_test.go: added `TestComparerBrListJSON_AllPaths` covering the production comparer shell-out (LookPath failure, command success with Dir+args asserted, command failure) via the `execLookPath`/`execCommand` package seams — coverage 90.8% → 96.9% (BrListJSON 0% → 100%).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/mirroraudit/ -count=1` — PASS (`ok ... 0.011s`).
- `go test ./internal/mirroraudit/ -count=1 -coverprofile` — coverage 96.9% (Audit 97.3%, BrListJSON 100%, parseJSONLLine 100%, add 100%, parseBrList 87.5%, Drift 100%).
- `go run ./tools/beads-mirror-audit --store /home/ajantha/projects/srv-jobfile/.beads` — "mirror clean (49 beads)", exit 0. PASS.
- `go build ./...` — PASS (BUILD_OK).
- `go vet ./internal/mirroraudit/` — clean.
- `MIRROR_AUDIT_SOT_ENFORCE=1 go test ./internal/mirroraudit/ -run TestSingleSource` — PASS (0 FAIL lines).
- Placeholder grep over internal/mirroraudit + tools/beads-mirror-audit — 0 hits.
- `git show --stat HEAD` — commit 3cf8255f, 2 files (+47).

## Open edges for the next phase
- parseBrList 87.5% (one envelope-error branch uncovered) — acceptable, failure path is fail-closed return.
- FR-4 Pan filing GREEN (pan_mirror_health filing contract) belongs to a sibling bead.
