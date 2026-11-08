# discovery handoff (hydra-1b1e)

## What changed (files + one line each)
- docs/discovery/stale-filing-sweep-context.md: discovery doc — inventories premise gate (`verifyPanFilingPremise`), `PrunableWedgeSweep`, `IntegrationExhaustedSweeper`; names the gap (in-progress filings whose premise cleared hold a cap slot); lists six design constraints for the implementation phase.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the discovery doc.
- docs/hydra/handoff/task-1b1e.md: this handoff.

## Verification (commands run + pass/fail)
- `cm context "stale-filing sweep covers in-progress filings with cleared premises" --json` — 1 relevant rule (`IntegrationExhaustedSweeper`, finalScore 0.4). PASS.
- `graphify query "Discovery: Stale-filing sweep covers in-progress filings with cleared premises" --budget 2000` — 215 nodes; key files surfaced: pan_health.go, pan_filing_premise_test.go, prune_sweep.go, discovery.go, service_starter.go. PASS.
- `rg` over internal/core/pan_health.go, internal/pan/prune_sweep.go, internal/pan/pan.go — confirmed `panFileSigTTL`, `FiledLabel`, `ErrFileBeadPremiseStale`, `PrunableWedgeSweep` contract text, and pan-filed exclusion from digest InProgress count. PASS.
- `sed`/`read` over CHANGELOG.md sections — `### Documentation` located; entry inserted with edit tool. PASS.

## Open edges for the next phase
- Implementation phase: add a periodic stale-filing closure sweep reusing the premise probes (source terminal, digest fresh, no live worker), emitting `pan_filing_closed_stale`, keeping the digest exclusions (pan-filed not counted in InProgress, completed-row skip) intact.
- Concrete design constraints for the next phase are enumerated in docs/discovery/stale-filing-sweep-context.md under "Design constraints for the implementation phase".
