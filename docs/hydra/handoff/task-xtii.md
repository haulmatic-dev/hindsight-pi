# refactor handoff (task-xtii — claim re-proof cache; gate-repair pass)

## What changed (files + one line each)
- cmd/worker/enrichment.go: `enrichBeadClaims` returns early (zero tokens, log line) when the bead has NO target files — previously an unscoped enrichment run executed the production runner, which in tests/paths sharing `HYDRA_PI_PATH` hijacked the enclosing rpc session's pi and hung `TestExecuteWithPi_RPCSuccessReturnsNilError` for 30s (fake pi never exits; enrich timeout is 10m). Also bounds cost: no targets = nothing to scope the agent to.
- CHANGELOG.md: Fixed entry under Unreleased.
- No change to internal/core claim-cache files — the REFACTOR (evaluateClaims unification) was already landed at commit 1e37712b and remains green.

## Verification (commands run + pass/fail)
- Gate log /opt/dev-tools/var/hydra/gates/hydra/hydra-xtii/test.log: two FAILs — `TestEnrichBeadClaims_BudgetElidesOldestWithFooter` (footer missing; NOT reproduced on re-run at HEAD — passed with the enrichment GREEN commit 35ac22e5 present) and `TestExecuteWithPi_RPCSuccessReturnsNilError` (executeWithPi did not return within 30s; reproduced twice, root cause above).
- `gofmt -l cmd/worker/enrichment.go` — clean.
- `go build ./cmd/worker/` — PASS (BUILD_OK).
- `ENRICH_ENFORCE=1 go test ./cmd/worker/ -run 'Enrich' -count=1` — ok (0.372s). PASS.
- `go test ./cmd/worker/ -run 'TestExecuteWithPi_RPCSuccessReturnsNilError' -count=1` — ok (1.512s). PASS (was 30.03s hang).

## Open edges for the next phase
- Enrichment-budget footer test passed on re-run; if it flakes again under gate load, check for concurrent file mutation of enrichment.go (this worktree is shared).
- Claim-cache chain state: RED/GREEN/SYNTAX complete; evaluateClaims refactor committed (1e37712b); no open edges.
