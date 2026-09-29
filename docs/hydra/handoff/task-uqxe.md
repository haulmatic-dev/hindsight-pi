# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-storm-uqxe.md: storm triage (2,090 events / 166 beads analyzed) restored onto HEAD — NOT serialized rebase contention. Three modes — (1) PR-already-exists retry loop (09-29, 49 beads; root cause fixed on development via publisher PR-reuse reconciliation, hydra-6zwl `resolveOrCreatePR`), (2) pre-push husky lint wedge (09-27, 903 events; already fixed via `--no-verify`), (3) merge conflicts in churn files + git index.lock contention (expected parks, retest stage owns).
- internal/core/publisher_pr_exists_test.go: 6 regression tests adapted to the landed seams (`ghPRView`, `prURLFromError`, HEAD's stricter `isPRAlreadyExists` requiring "a pull request for branch" phrasing) — pin create-failure reuse, URL-from-error path, ghPRView fallback, fail-closed passthrough.
- docs/hydra/handoff/task-uqxe.md: this handoff.
- CHANGELOG.md: already carries the Fixed entry (restored from da22ca0 content).

## Provenance
- Original work committed as da22ca0 on this worktree; that commit ended up on
  `hydra/quarantine/hydra-uqxe` (not HEAD) after parallel chains
  (hydra-bwyn, hydra-6zwl) landed an equivalent — and better — publisher fix.
- Code fix is SUPERSEDED by HEAD's implementation (superset: state-checked
  `ghPRView`, URL extracted from the create error itself). Only docs + tests
  re-landed here; no conflicting code change.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go test ./internal/core/ -run 'TestIsPRAlreadyExists|TestPrURLFromError|TestResolveOrCreatePR' -count=1` — PASS (6 tests).
- `gofmt -l internal/core/publisher_pr_exists_test.go` — clean.

## Open edges for the next phase
- SECURITY: integration-exhausted event details embed the authenticated remote URL with the GitHub PAT (x-access-token:…); push path redacts, the integrate-failure formatter does not — needs redaction at that site.
- Tier B agentic conflict resolution still unimplemented (pmgh quarantine merge conflict is human merge).
- With the PR-reuse fix landed, retest stage should auto-recover PR-already-exists-parked beads whose merge-tree is clean; observe next daemon tick.
- PR base for telematics closes is `feat/intergration-test-plan` (typo'd feat branch) — config knob `integration_branch` can repoint if that branch is retired.
