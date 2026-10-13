# green handoff (fix-ot9a)

## What changed (files + one line each)
- internal/core/graphify_path_guard.go (new): path-existence guard — atomic TTL-gated `project_path_missing` event, exactly once per 24h per path.
- internal/core/graphify_indexer.go: `Refresh` stats the path before claim (nonexistent → no build goroutine, no claim map entry); `missingPaths` map + `SetEventSink` wiring.
- internal/core/overseer.go: `NewOverseer` attaches the daemon EventSink to the graphify indexer.
- internal/core/graphify_path_guard_test.go (new): 4 tests — nonexistent path refused, event once per TTL, reappearing path refreshes, existing path refreshes.
- CHANGELOG.md: Fixed entry under Unreleased.
- docs/hydra/handoff/green-2spi.md: original GREEN handoff (restored with the re-land).

## Provenance
- Verify-first found the entire GREEN implementation already complete on local branch `hydra/wt-hydra-r98k` as commit d73460ab (re-land of 8e64d4aa+40def070 after a worktree reset); it was NOT on HEAD or origin/development. This bead cherry-picked d73460ab onto HEAD (343b7377) as 8c4eb7bf.
- Note: this is a cherry-pick, not a fresh implementation; prior fix bead hydra-z4x3 had already landed the same mechanism on a prior strand.

## Verification (commands run + pass/fail)
- `go build -buildvcs=false ./...` — PASS ("Build successful").
- `go test ./internal/core/ -run 'TestGraphifyPathGuard' -count=1 -v` — PASS 4/4 (NonexistentPathRefused, EventEmittedOncePerTTL, ReappearingPathRefreshes, ExistingPathRefreshes).
- `git show --stat 8c4eb7bf` — 6 files, +271/−3, all claimed files present in diff.

## Gate-failure remediation (this round)
- Daemon full-suite gate FAILed on 4 pre-existing RED-scaffold tests (TestPostCloseSweep*, postclose_sweep_test.go: scan() inert stub, unrelated to graphify guard).
- Root cause: GREEN implementation of PostCloseSweeper.scan never landed on HEAD; it existed as commit 180822cb (on quarantine branches hydra/uppd-96th lineage, itself a cherry-pick of 0001d216).
- Remedy: cherry-picked 180822cb onto this worktree as 56899214 — implements reopen/flake-guard/needs-human scan + CHANGELOG entry.
- `go test ./internal/core/ -run 'TestGraphifyPathGuard|TestPostCloseSweep' -count=1 -v` — PASS 10/10.

## Open edges for the next phase
- Landing: d73460ab also exists on hydra/wt-hydra-r98k — integration should take this worktree's cherry-pick (8c4eb7bf) and NOT merge hydra/wt-hydra-r98k wholesale (that branch carries unrelated FR-005 / graphify-symlink strands).
- `SetEventSink` is only wired in `NewOverseer`; indexers constructed elsewhere (tests) keep a nil sink (no-op, by design).
