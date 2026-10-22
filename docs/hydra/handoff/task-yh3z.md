# discovery handoff (yh3z / FR-002)

## What changed (files + one line each)
- docs/discovery/mirror-health-fr2-discovery.md: discovery for FR-002 periodic read-only mirror audit sweep — design modeled on `internal/core/pan_stale_filing.go` (interval-throttled overseer tick, probe seams, typed events, disk ledger), knobs `mirror_health.enabled` / `mirror_health.audit_interval`, guards (mtime < 2m quiescence, live-worker), AC mapping table + RED test plan, ambiguities resolved (reuse beads-mirror-audit parsing, ledger restart-survival, live-worker definition, no auto-heal in this bead).
- CHANGELOG.md: Documentation entry under Unreleased pointing at the discovery doc.

## Verification (commands run + pass/fail)
- `cm context "periodic read-only audit sweep mirror_health config knobs" --json` — CASS queried; no matching anti-pattern (only unrelated bv-client rules). PASS.
- `graphify query "Periodic read-only audit sweep mirror_health" --budget 2000` — graph present; FR-002 node maps to docs/PRDs/mirror-health-prd-20261022.md; runAudit/TestAudit_Readonly nodes confirmed in tools/beads-mirror-audit. PASS.
- `rg -n "mirror_health|MirrorHealth" internal/` — zero hits (knobs/implementation do not exist yet; RED/GREEN scope confirmed unclaimed). PASS.
- `hindsight_search at bead start` — command not available on this host (skip rule applied).
- Live-worker definition verified: overseer computes `activeWorkerKeys()` (internal/core/overseer.go) — sweep should reuse it, not a second liveness source.

## Open edges for the next phase
- RED bead: write `internal/core/pan_mirror_health_test.go` (contract tests per discovery doc) + inert stub `internal/core/pan_mirror_health.go` with `MIRROR_HEALTH_ENFORCE=1` gate, per the pan_stale_filing / bead_claims precedent.
- GREEN bead: implement sweep + knobs in `internal/core/config.go`, wire on overseer tick, ledger at `$HYDRA_HOME/mirror-health-ledger.json`.
- Sequencing: FR-1 (`internal/mirroraudit` package) may land first or after; sweep must reuse existing parsing either way (import package or shell CLI via injected seam) — never a second parser.
