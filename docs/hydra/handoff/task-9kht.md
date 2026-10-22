# red handoff

## What changed (files + one line each)
- internal/core/pan_mirror_health_test.go: new RED tests — sweep gate (interval firing: not-due/due/disabled; mtime-quiescence skip; live-worker skip), positive control (eligible store audits + `mirror_audit_clean`, error fail-open + `mirror_audit_error`, typed `mirror_drift_detected` per divergent record) behind `mirrorHealthSweepRequireImpl` / `MIRROR_HEALTH_ENFORCE=1`.
- internal/core/mirror_health_config_test.go: knobs `mirror_health.enabled` / `mirror_health.audit_interval` parse, defaults (enabled true, 30m), zero interval disables.
- internal/core/mirror_health_docs_test.go: docs/SLO contract tests skip loudly pre-GREEN via `mirrorHealthDocsRequireImpl` (`MIRROR_HEALTH_DOCS_ENFORCE=1` fails them).
- CHANGELOG.md: Added entry under Unreleased.
- docs/hydra/handoff/task-9kht.md: this handoff.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'MirrorHealth' -count=1` — PASS, exit 0 (sweep contract subtests skip loudly pre-GREEN; config tests green; guardrail tests green).
- `MIRROR_HEALTH_ENFORCE=1 go test ./internal/core/ -run 'MirrorHealth' -count=1` — FAIL with exactly the sweep-contract assertion errors (`mirroraudit.RunAudit not yet implemented`) — proves RED detects the missing sweep.
- `gofmt -l` on touched files — clean. `go vet ./internal/core/` — clean.
- Commit 196af1cc (2 files, +60).

## Open edges for the next phase
- GREEN must implement the sweep body (mirroraudit.RunAudit wiring, mtime + live-worker probes, typed events) and flip the impl gate; keep exported shapes byte-identical.
- Docs/SLO tests still skipping under `MIRROR_HEALTH_DOCS_ENFORCE=1` until shepherd docs + SLO script land.
