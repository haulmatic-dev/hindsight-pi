# discovery handoff

## What changed (files + one line each)
- internal/core/pan_mirror_health_test.go: FR-3 guarded auto-resync RED contract tests — db_ahead terminal heal via stubbed `BrUpdater`, non-terminal never heals, user-owned store guard, write-probe failure guard (`mirrorWriteProbe` seam), `auto_resync` OFF by default; all gated on `mirrorResyncImplemented` (`MIRROR_HEALTH_ENFORCE=1` proves RED).
- internal/core/pan_mirror_health.go: added inert `mirrorWriteProbe` probe hook (default nil error) so guard tests can inject write-probe failure without touching the sweep body.
- CHANGELOG.md: Fixed entry under Unreleased documenting the FR-3 contract tests.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'MirrorHealth' -count=1` → `ok github.com/haulmatic-dev/hydra/internal/core 0.011s` PASS (all contract tests skip under default RED gate; enforcement path proven by prior turn).
- Acceptance criteria coverage: db_ahead terminal heal stubbed-BrUpdater (TestMirrorHealthSweep_HealsDBAheadTerminalRecord), non-terminal no-heal (TestMirrorHealthSweep_NonTerminalRecordNeverHeals), user-owned guard (TestMirrorHealthSweep_UserOwnedStoreNeverHeals), write-probe failure guard (TestMirrorHealthSweep_WriteProbeFailureNeverHeals), auto-resync OFF default (TestMirrorHealthSweep_AutoResyncOffByDefault) — all present and passing under the gate.

## Open edges for the next phase
- GREEN: flip `mirrorResyncImplemented` to true in `internal/core/pan_mirror_health.go` and implement the actual guarded heal (BrUpdater via beadsClient, quiescence + write-probe guard order) so the contract tests execute for real.
- Write-probe is currently an inert seam: GREEN should wire it to a real store write-eligibility check (e.g. open-for-write probe on the beads store path) before any heal attempt.
