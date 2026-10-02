# task handoff

## What changed (files + one line each)
- internal/core/pan_health.go: fleet-digest live-worker attribution now lists all registry states and counts a heartbeat-fresh row (<5m) as live regardless of state — fixes the P0 orphan-claim false positive that made Pan file a twin report bead every judgment round.
- internal/core/pan_health_test.go: regression test TestCollectHealthDigestFreshHeartbeatCompletedRowNotOrphan (heartbeat-fresh completed row → OrphanClaims=0, no P0 anomaly).
- CHANGELOG.md: Fixed entry under Unreleased.
- docs/hydra/handoff/task-wv2e.md: this handoff.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (clean); `gofmt -l` on changed files — clean.
- `go test ./internal/core/ -run 'TestCollectHealthDigest' -count=1` — PASS (6 tests incl. new regression; initial run RED confirmed the bug reproduces, fixed, then GREEN).

## Verified findings (P0 orphaned in_progress claims, hydra-wv2e)
- reconcileOrphanClaim (bb75c4a, PR #59) IS on origin/development and in the deployed daemon binary (strings match: reconcileOrphanClaim, orphan_claim_reset, OrphanClaims digest field). Charon orphan recovery live (fmsfe-sn5 reopened 00:52; hydra-1p1q 02:45:25, hydra-vk94 02:47:24).
- Root cause of the PERSISTING anomaly: digest liveness filter mismatch. Registry row for hydra-l867: state=completed (prematurely, 02:52:38) but LastHeartbeat fresh (9s at 03:10) — the worker process (PID 3896762) was alive and heartbeating. Charon's beadHasLiveWorker counts fresh heartbeat as live → correctly skipped. Pan digest's liveByKey = ListWorkers(working,parked) → l867 excluded → in_progress + no live row → OrphanClaims++ → "P0 campaign anomaly" → Pan file-bead → twin bead (this bead and siblings) every round.
- Current live state: 0 true orphan claims; all other in_progress beads have working-state rows with fresh heartbeats; pipeline healthy (9/10 workers, DAG ready beads present).
- Disk: `df -h /` = 185G/451G = 43% — the 99% claim in the bead body is stale (also refuted in mvcx/fdd9/1p1q handoffs). No systemd-tmpfiles cleanup required.
- Wedged bead IDs for human review: none requiring review — the only residual, hydra-l867 (gate-build-failed rejection round 1/3, worker still finalizing), is actively worked, not wedged.

## Open edges for the next phase
- Root race: registry row marked completed while worker process still runs (waitForWorker/completion path). Digest fix removes the false alarm; consider investigating why the completion path marks rows early.
- Charon grace keys on bead.UpdatedAt — any bead touch resets the 5m orphan clock (conservative direction, known).
