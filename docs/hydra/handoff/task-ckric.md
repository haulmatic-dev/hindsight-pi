# Handoff — task-ckric

- Bead ID: ckric
- Phase: refactor
- Date: 2026-07-31 (dogfood)
- Branch: hydra/wt-hydra-b8dy

## What this bead is

REFACTOR: Optimize Coverage measurements in decision packets and events. Behavior-preserving
cleanup of `internal/core/coverage_packet.go` / `coverage_packet_test.go`, all
`TestCoveragePacket_` tests must pass.

## State at start (superseded)

The refactor this bead describes was already landed by the prior bead on this
worktree (task-b8dy, commit `95ac53ed`): `metricName(i)` helper + extracted
`isBreachingCell`/`anyCellBreaches` predicates. The RED deliverable of this
chain was superseded by the GREEN implementation (commit `c64a8cbf`), and the
`COVERAGE_PACKET_ENFORCE` gate was removed earlier in the chain.

## Work done in this bead

Verify-first close plus one in-scope fix:

1. Ran `graphify query` (no new coverage-packet nodes relevant; decision-packet
   nodes concern `pan_decision_packet.go`, not this path).
2. Narrow tests: `go test ./internal/core/ -run 'TestCoveragePacket_' -count=1`
   → `ok` (10/10 pass).
3. `gofmt -l` flagged `internal/core/coverage_packet_test.go` (map-literal and
   table-literal alignment drift introduced by the earlier test commit).
   `gofmt -w` applied — formatting only, no behavior change.
4. `go vet ./internal/core/` clean. Changelog and prior-bead handoff already
   present.

## Out-of-scope finding (recorded, not fixed)

`gofmt -l` also flags `internal/core/worktree_reconciler.go` as unformatted —
pre-existing drift outside this bead's scope. Recorded via `cm add`; needs its
own bead.

## Known follow-up (unchanged)

`FromMeasurements` mutates its input map (deletes the `"all_files"` key). Pinned
by `TestCoveragePacket_FromMeasurements_PopulatesFields`; if any caller relies on
the map surviving, file a new bead for that behavior change.

## Verification

```
go test ./internal/core/ -run 'TestCoveragePacket_' -count=1  → ok
gofmt -l internal/core/coverage_packet_test.go               → clean
go vet ./internal/core/                                      → clean
```
