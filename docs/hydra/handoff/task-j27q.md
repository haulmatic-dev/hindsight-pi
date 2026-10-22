# red handoff (hydra-j27q)

## What changed (files + one line each)
- internal/core/mirror_health_docs_test.go (new): 10 RED contract tests for PRD FR-5 "Config docs, runbook line, SLO check", gated by `mirrorHealthDocsRequireImpl` (skip loudly pre-GREEN, `MIRROR_HEALTH_DOCS_ENFORCE=1` fails them):
  - SLO script (5): `bash -n` + healthy fixture exits 0 with `SLO-OK`; drift fixture exits 1 with `SLO-BREACH` naming `mirror_drift_detected`; registered store without a recent `mirror_audit_clean` exits 1 naming the missing coverage; missing tools exit 2 per the header contract; read-only byte-compare proves the store `issues.jsonl` is never mutated.
  - Docs (4): `docs/architecture/shepherd.md` names all FR-2 knobs (`mirror_health.enabled`, `mirror_health.audit_interval`, `auto_resync`) with the OFF-by-default posture pinned; `AGENTS.md` operating-knobs table covers `mirror_health`; the runbook (shepherd.md or host-setup.md) carries the post-repair line `br sync --import-only`.
- CHANGELOG.md: Added entry under Unreleased describing the suite + current RED failure reasons.
- Changed helper `runSLOScript` builds a hermetic env (`HYDRA_EVENTS_DIR`/`HYDRA_CONFIG` overrides layered over `os.Environ()`, overridden keys deduped) so ambient host state cannot leak into fixtures.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS (exit 0).
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l internal/core/mirror_health_docs_test.go` — clean.
- `go test ./internal/core -run 'TestMirrorHealthDocs|TestMirrorHealthSLOScript'` — PASS (10 SKIPs loudly; daemon gate green pre-GREEN).
- `MIRROR_HEALTH_DOCS_ENFORCE=1 go test ./internal/core -run 'TestMirrorHealthDocs|TestMirrorHealthSLOScript' -v` — PASS: 5 / FAIL: 5, all five for the RIGHT reason:
  - shepherd.md does not name `mirror_health.enabled` / `auto_resync` (missing doc content).
  - AGENTS.md operating table does not cover `mirror_health`.
  - runbook line `br sync --import-only` absent from shepherd.md and host-setup.md.
  - SLO script exits 127 (`date: command not found`) instead of 2 on missing tools — GREEN must add a required-tools probe (`command -v date/jq`) and exit 2.
- CASS `cm context` queried (Step 1): no anti-pattern matched this approach.

## Open edges for the next phase
- GREEN must implement: (a) the four doc deliverables (shepherd.md knobs + auto_resync OFF-by-default, AGENTS.md knob row, runbook `br sync --import-only` line), and (b) the SLO script required-tools probe so missing `date`/`jq` yields exit 2, not bare command-not-found 127.
- `TestMirrorHealthSLOScript_SyntaxAndHealthyFixture` uses `t.TempDir()` paths from the fixture config; keep event store path equal to registered store path when refactoring the fixture helper.
