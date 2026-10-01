# bug handoff

## What changed (files + one line each)
- internal/core/scheduler.go: `getRetryEligibleBeads` exhaustion branch now reconciles orphaned claims — a retry-exhausted bead sitting `in_progress` in the DAG with no live registry worker and no hard-park label (escalated/worktree-unavailable/integration-exhausted) is reset to `open`, its retry entry cleared (one fair retry), and a typed `orphan_claim_reset{bead,last_worker,attempts}` event emitted via new `reconcileOrphanClaim` + `emitOrphanClaimReset`. Fixes the fms wedge: 6 repair beads stuck in_progress after retry-exhaustion, invisible to scheduler AND blocking dependents.
- internal/core/worker_spawner.go: new `LastWorkerForBead(beadID)` — returns `pid:<pid>` of the bead's registry row (if any) for claim attribution in typed events.
- internal/core/pan_health.go: digest counts `OrphanClaims` (in_progress beads with no matching live registry row) per project and emits the P0 campaign anomaly `"P0 campaign anomaly: N in_progress bead(s) with no live registry worker"`.
- internal/pan/health.go: `ProjectHealth.OrphanClaims` field; `Text()` renders `orphanclaims=%d`.
- internal/pan/pan.go: judgment prompt gains an orphaned-claim rescue rule (same P0 class as fleet stall; mechanical fix is scheduler/Charon recovery; persistent anomaly → file-bead naming wedged bead IDs).
- internal/core/scheduler_orphan_claim_test.go: 3 tests — orphaned in_progress claim reset to open + retry cleared + one `orphan_claim_reset` event with last_worker field; hard-parked labels never reset; non-orphan (open) beads untouched.
- internal/core/pan_health_test.go: `TestCollectHealthDigestOrphanClaims` — orphan counted, P0 anomaly emitted, digest text shows orphanclaims.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/ ./internal/pan/` — PASS (clean).
- `gofmt -l` on changed files — clean.
- `go test ./internal/core/ -run 'TestScheduler_OrphanClaimReset|TestCollectHealthDigestOrphanClaims' -count=1` — PASS (4 tests).
- `go test ./internal/core/ -run 'TestScheduler|TestRetry|TestCollectHealth|TestPan|TestRecoverOrphan|TestCharon' -count=1` — PASS (58 tests).
- `go test ./internal/pan/ -count=1` — PASS (99 tests).

## Open edges for the next phase
- "Reset status atomically": reset is store-level `UpdateBeadStatus(open)` + in-memory `ClearRetry`; not a single br transaction — acceptable (br has no atomic status+counter op), but a crash between them leaves an open bead with an exhausted counter, which the existing DAG scheduler picks up as ready.
- Charon orphan recovery (5m grace) unchanged — it remains the backstop for worker deaths with no retry-exhaustion entry; the new sweep covers the retry-exhaustion gap Charon cannot see (no registry row → already handled, but exhausted budget made the reopen futile before; now `ClearRetry` makes the reopen effective).
- Live e2e: watch the next fms-like campaign for `orphan_claim_reset` events and confirm respawn follows.
