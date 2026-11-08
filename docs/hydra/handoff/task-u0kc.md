# discovery handoff (task-u0kc, FR-005 turn distribution extractor)

## What changed (files + one line each)
- docs/discovery/turn-report-extractor-discovery-u0kc.md: discovery — verified the staged inert RED surface (`tools/turn-report/main.go` stub + 9-test contract suite), resolved percentile/budget/zero-events/drift ambiguities for GREEN, mapped ACs, and flagged the chain blocker: `cmd/worker/turn_telemetry_test.go` imports the nonexistent `internal/types` package, breaking the cmd/worker test build (`[setup failed] no required module provides package github.com/haulmatic-dev/hydra/internal/types`).
- CHANGELOG.md: Documentation entry pointing at the discovery doc.
- No production code change (discovery bead; GREEN obligations pinned in the doc).

## Verification (commands run + pass/fail)
- `go test ./tools/turn-report/ -count=1` — PASS (`ok ... 0.003s`; AC4 met at discovery — RED suite skip-loud green).
- `TURN_REPORT_ENFORCE=1 go test ./tools/turn-report/ -count=1` — FAIL as designed (RED proven; failure reason = contract not implemented, not compile error). PASS (evidence).
- `go vet ./tools/turn-report/` — clean.
- `go test ./cmd/worker/ -count=1` — `[setup failed]` due to the sibling strand's nonexistent `internal/types` import (documented, not fixed here — out of FR-005 scope).
- hindsight_search at bead start — not connected (skip rule applied).

## Open edges for the next phase
- GREEN: flip `turnReportImplemented`, implement `run()` per the pinned contract (nearest-rank percentiles p25/p50/p75/p90/max per `fields.phase`, `turn_budgets:` COMMENT round-up-to-10 of p90, `insufficient data: N events` exit 0 fail-open, drift warning n<20, `--days` window, `TURN_REPORT_NOW` test seam).
- RED/GREEN peers on the telemetry chain must fix the `internal/types` import or create the package — cmd/worker's whole test build is broken until then.
- Do not weaken the 9 pinned assertions in `tools/turn-report/main_test.go`.
