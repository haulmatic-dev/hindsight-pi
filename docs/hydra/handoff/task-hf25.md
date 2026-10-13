# task handoff (hydra-hf25)

## What changed (files + one line each)
- internal/core/pan_health.go: `collectDiskWatermark` units bug fixed — `fs.Bfree` is in blocks but was mixed with a byte-scaled `total = Blocks × Bsize`, yielding a constant ~99% used on ANY filesystem. Root cause of the fleet-wide recurring disk-99% campaign anomaly. Now block-vs-block: `(Blocks − Bfree) × 100 / Blocks`, zero-block guard, incident comment.
- internal/core/pan_health_test.go: new `TestCollectHealthDigestDiskUsedPctMatchesDf` — digest `DiskUsedPct` must be within ±2 of the real statfs block ratio (RED against the bug: old code gave 99 vs actual ~38 on this host).
- CHANGELOG.md: Fixed entry + Documentation triage entry.
- docs/discovery/orphanclaim-disk99-units-bug-triage-hf25.md: full triage — all three P0 claims dispositioned; true disk root cause identified.

## Verified triage outcomes (live, 2026-10-06 15:0x UTC)
- Orphans: 19 in_progress beads all have registry rows with fresh heartbeats (self `working`; others `completed` in grace); escalated q207/rxi6 and pan-filed meta excluded by design. Charon live: 64 `charon_orphan_reopened` today, latest 15:05:30Z. 0 live orphans.
- Publisher wedge: local checkout HEAD 343b7377 == origin/development, 0 unpublished commits, no open publish PR. Wedge absent.
- Disk: host `df -h /` = 40% used (ext4, inodes 20%) — no I/O-error-28 risk; `systemd-tmpfiles --clean` run (rc=0).
- Go probe reproduced the bug exactly: `syscall.Statfs_t` blocks=118,074,338 bfree=73,393,771 → buggy formula printed 99, fixed formula ~38.
- Live-store: `br doctor migrate-schema recover` cleared WAL poison; twins hydra-z0do/m3bj/o331/284j/9ufq closed `not_planned: duplicate of hydra-hf25` with triage comments (✓ ×10).
- Deployed binary `/opt/dev-tools/var/hydra/bin/hydra` (built 12:16) carries the buggy formula → operator must rebuild + drain-restart for the fix to stop the ~2–4/min digest twin filings.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l internal/core/pan_health.go internal/core/pan_health_test.go` — clean.
- `go test ./internal/core/ -run 'TestCollectHealthDigestDiskUsedPctMatchesDf|TestCollectHealthDigestDiskWatermark|TestDiskCleanupGuard' -count=1` — PASS (ok 30.0s).
- Python statvfs cross-check: 37% (bfree) / 41% (bavail) — matches fixed digest math.

## Open edges for the next phase
- OPERATOR: rebuild daemon from this fix and drain-restart (`kill -TERM <pid>`, systemd Restart=always) — until then the digest keeps filing disk-99% twins and running tmpfiles cleanup every ~2 min.
- Digest filing cadence (~2–4 min, 7 twins today at 14:13–15:01) remains hot; consider a per-signature cooldown in filePanBead beyond title dedupe.
- Cleanup pass scoped by i6we handoff is intentionally tmpfiles-only; repeated 99% breaches under the fixed math would be real.
