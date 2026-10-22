# Discovery handoff — FR-001 mirror audit single source of truth

## What changed (files + one line each)
- internal/mirroraudit/audit.go — shared audit package (classification + record scan) used by daemon sweep and CLI tool.
- internal/mirroraudit/audit_test.go — fixture tests for clean/divergence/missing classifications.
- internal/mirroraudit/runner.go — br binary invocation seam (prod = execBr, fail-open on absence).
- internal/core/pan_mirror_health.go — daemon periodic sweep using the shared package; events mirror_drift_detected/mirror_audit_clean.
- internal/core/pan_mirror_health_test.go — sweep RED/inproc tests.
- internal/mirroraudit/single_source_test.go — DRIFT guard: asserts CLI tool imports internal/mirroraudit (single-supplier regression).
- tools/beads-mirror-audit/main.go — CLI wrapper now a thin shell over the shared package.

## Verification (commands run + pass/fail)
- `go test ./internal/mirroraudit/ -count=1` — ok (0.012s). PASS.
- `go run ./tools/beads-mirror-audit --store /home/ajantha/projects/srv-jobfile/.beads` — "mirror clean (49 beads)", exit 0. PASS.
- `go build ./...` — BUILD_OK. PASS.
- Read-only contract: execBr only runs `br list --json`; no fs mutation calls in audit paths.

## Open edges for the next phase
- pan_mirror_health.go wiring: confirm the sweep is registered in service_starter.go with the `mirror_health.enabled` config knob; untracked files in this worktree must be committed (git status shows them).
- If br binary output envelope shape changes (`{"issues": [...]}` wrapper), runner parsing must be updated; failing that gate is exit 2 audit-unavailable (fail-open), not drift.
