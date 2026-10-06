# discovery handoff (hydra-xcye)

## What changed (files + one line each)
- internal/core/pan_health.go: digest OrphanClaims counter now skips in_progress beads whose registry row is `state=completed` (new `completedByBead` map from the same ListWorkers scan) — a completed row is terminal worker state; the scheduler's respawn backoff owns the requeue, and counting the bead during that window re-fired the P0 campaign-anomaly filing loop every sweep. Genuine orphans (no row past the 5m claim grace; working/hung row with stale heartbeat) still count.
- internal/core/pan_health_test.go: TestCollectHealthDigestCompletedRowNotOrphan — completed+stale row → not orphan; completed+fresh row → not orphan; no row → orphan + P0 anomaly fires.
- docs/discovery/orphanclaim-completed-row-triage-xcye.md: triage — the "15 orphaned claims" were gate-reject-churn + red/discovery beads, every one with a completed worker row; publisher wedge absent (PR #268 merged, local ahead=0); disk 50%→39% after systemd-tmpfiles --clean (stale claim).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- Registry `$HYDRA_HOME/.hydra/registry.db`: 25 in_progress beads; all 16 non-excluded candidates (bldo/lb7g/e2a7/jdvp/7tzf/dyw2/gcal/m72l/n9nq/mt7e/7w2f/uppd/ngui/96th/xmla/n5rv) have rows, all `state=completed` heartbeats 13:18–13:41 → digest's 15 = completed rows aged past the 5m liveness window. VERIFIED.
- Events 2026-10-06.jsonl: 48 charon_orphan_reopened (last 13:34, dyw2/gcal/m72l cycled); 5 pan_bead_filed campaign-anomaly twins 13:41–13:53. Ladder progressing, filing loop confirmed. PASS.
- `git rev-list --count origin/development..HEAD` on main checkout = 0; remote tip 53eca1b = merge of PR #268 → publisher wedge ABSENT. PASS.
- `df -h /` → 50% at start, 39% after `systemd-tmpfiles --clean` — 99% claim stale. PASS.
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestCollectHealthDigestCompletedRowNotOrphan'` — PASS.
- `go test ./internal/core/ -run 'TestCollectHealthDigest|TestRecoverOrphan|TestGateClose'` — 25/25 PASS.

## Open edges for the next phase
- Deploy: fix is in-branch; digest stays noisy until the daemon runs a binary containing this change (recurring stale-binary trap — check deployed binary mtime vs origin/development tip first).
- q207/rxi6 escalated beads still need human/Pan clear-escalation (by-design exclusion, unrelated to this fix).
- A stale working/hung row still counts live under digest semantics — deliberate (existing behavior, not changed here).
