# fix handoff (fix-vn9s)

## What changed (files + one line each)
- internal/core/charon.go: removed the park-gate `continue` in `recoverOrphanInProgressBeads` — orphan recovery (stale-row reap + orphan-bead reopen) now runs for parked projects; spawn decisions stay gated by the scheduler via ProjectParkInfo.
- internal/core/charon_orphan_test.go: replaced `TestRecoverOrphanInProgressBeads_SkipsParkedCampaign` with `TestRecoverOrphanInProgressBeads_ParkedCampaignStillRecovers` — parked project + dead-PID tracker row → bead reopened (`br update <id> --status open`), ghost row reaped.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS
- `go test ./internal/core/ -run 'TestRecoverOrphan|TestCharon' -count=1` — PASS (24 tests)

## Open edges for the next phase
- Live: park a campaign with a dead worker, confirm next Charon sweep reaps the row and the "6/6 slots filled" stall does not recur.
- The 2026-09-27 "reopen respawned through park" comment is obsolete — scheduler-side ProjectParkInfo gating is the only park enforcement now; if any spawn path lacks that check, reopened parked beads could respawn.
