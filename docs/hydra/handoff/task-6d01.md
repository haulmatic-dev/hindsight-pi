# task handoff (hydra-6d01)

## What changed (files + one line each)
- internal/core/pan_health.go: OrphanClaims digest counter now skips beads carrying the `escalated` label — human-gated in_progress beads never get a worker and must not re-fire the P0 orphan-claim anomaly (labelsByID map added).
- internal/core/pan_health_test.go: `TestCollectHealthDigestEscalatedBeadNotOrphan` — escalated in_progress bead with no worker row → OrphanClaims 0, no P0 anomaly.
- docs/discovery/orphanclaim-escalated-triage-6d01.md: triage — the 4 named beads (bxzr/1h4v/wiu6/cxpf) are tdd-validation-failed human escalations, not orphans; disk 43% (claim stale); Charon reaping live-confirmed.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `df -h /` → 183G/451G = 43% (watermark 85%) — disk-99% claim false. PASS.
- registry.db query — 7 working rows, heartbeats <1 min; zero live orphans; bxzr/cxpf workers completed exit 0 before escalation. PASS.
- events 2026-10-02.jsonl — 4 `bead.escalated` (tdd-validation-failed) 07:20–08:02; 2 `charon_orphan_reopened` (bxzr 07:47, cxpf 07:50). PASS.
- `br doctor migrate-schema recover` — cleared recurring WAL poison ("database is busy (recovery in progress)"); second attempt succeeded. PASS.
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestCollectHealthDigest' -count=1` — PASS (ok).
- Branch: dogfood/orphan-digest-escalated off origin/development tip 76625e7 (worktree was 37 commits behind dev; stale uncommitted residue from landed PR #81 work discarded after confirming ancestry).

## Open edges for the next phase
- The 4 escalated GREEN beads (bxzr/1h4v/wiu6/cxpf) still need human/Pan disposition — they will re-fail tdd-validation on a naive clear-escalation retry; consider abandon-with-summary per the escalation recipe if unchanged.
- br WAL-index poison recurs multiple times per day on the hydra store; root cause (concurrent daemon+CLI writers) still unaddressed.
- Deployed daemon binary needs a rebuild/restart to pick up this digest exclusion.
