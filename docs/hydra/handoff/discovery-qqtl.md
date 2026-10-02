# discovery handoff (hydra-qqtl)

## What changed (files + one line each)
- internal/core/pan_health.go: pan-filed beads excluded from digest InProgress count — kills self-referential orphan-claims loop (inprog−workers phantom) at the source; plus pre-existing gofmt drift at pan_bead_filed emit fixed.
- internal/core/pan_health_test.go: TestCollectHealthDigestPanFiledNotCountedInProgress.
- docs/discovery/orphan-claims-recurrence-triage-qqtl.md: triage — snapshot, root cause, live-store dispositions.
- CHANGELOG.md: Unreleased entry.

## Live-store actions (verified via br output)
- Closed as duplicate twins: hydra-12di, hydra-s9sg, hydra-1euz (pre-closed), hydra-yz8z, hydra-t2e1, hydra-qmf7, hydra-wv2e, hydra-oqp8 (twin filed mid-triage), each with triage comment.
- br WAL poison recurred ("database is busy (recovery in progress)"); `br doctor migrate-schema recover` cleared it.
- Charon verified recovering real orphans today: 3× charon_orphan_reopened (dead_worker) on 2026-10-02.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (no output).
- `gofmt -l internal/core/pan_health*.go` — clean.
- `go test ./internal/core/ -run 'TestCollectHealthDigest|TestParkCampaign|TestUnpark' -count=1` — PASS.
- `go test ./internal/pan/ ./internal/core/ -run 'TestFilePanBead|TestFileBead|TestCollectHealthDigest|TestPanRepair' -count=1` — PASS both.
- Disk `df -h /` = 38% — 99% claims stale. Registry workers table 0 rows between turns (normal); no real orphan at triage.

## Open edges for the next phase
- After deploy, phantom orphanclaims should stop. A future orphanclaim with 0 pan-filed in_progress beads = REAL orphan → name bead ID, Charon path verified working.
- hydra-l867: genuinely wedged work bead (gate-failed-build + VCS status 128) — separate human triage, not part of this anomaly.
