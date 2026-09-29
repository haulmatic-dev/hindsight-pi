# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-2vuc.md: triage of the mass integration-exhausted hard-parks (hydra-xxq5 18-bead cluster + ajantha-srv-telematics-burr) — root cause poisoned br WAL index, retry backoff already landed, dispositions per bead.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.
- No code change (discovery/triage bead; retry-policy proposal already implemented upstream as integration_retest + integration_gate).

## Verification (commands run + pass/fail)
- `br doctor migrate-schema recover` ×2 on /opt/dev-tools/components/hydra/.beads — both PASS ("Recovered engine read admission"); before recovery, `br show hydra-xxq5` failed with "engine recovery failed at live-recovery ... database is busy (recovery in progress)".
- `br show hydra-xxq5` — OPEN, mass-park meta bead; parks NOT cleared (constituent superseded branches need individual disposition).
- `br show ajantha-srv-telematics-burr` — IN_PROGRESS, closed-failed+pan-filed, prior triage comments (qzxv/movi/8odx) record cause-fixed + labels-cleared + loop=abandon-with-summary.
- `go build ./...` in hydra-2vuc worktree — PASS; `go vet ./internal/core/` — PASS (gate failure in bead description was the 6zwl isPRAlreadyExists redeclaration, fixed by commit e0c70a6 present in this worktree).
- hindsight_search on shared-wedge terms — durable-park-by-design + shared-wedge playbook rules confirmed.

## Open edges for the next phase
- Human/Pan: disposition hydra-xxq5's constituent parks individually (abandon-with-summary for superseded; clear-hard-park only where content landed) — xxq5 itself can then close as duplicate of hydra-qzxv.
- Human: close ajantha-srv-telematics-burr (IN_PROGRESS meta bead) with the abandon-with-summary note already drafted in its comments.
- Tier B agentic conflict resolution still unimplemented (separate bead).
