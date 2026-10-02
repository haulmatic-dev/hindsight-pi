# implement handoff (hydra-i6we)

## What changed (files + one line each)
- internal/core/disk_cleanup.go (new): `diskCleanupGuard` — when digest shows used% ≥ watermark, run `systemd-tmpfiles --clean` (30s timeout, no rm) once per hourly cooldown; fail-open; typed `disk_cleanup_run` event via daemon EventSink.
- internal/core/pan_health.go: collectHealthDigest invokes `o.diskCleanup.maybeCleanup` after `collectDiskWatermark` (lazy-constructs guard for tests without NewOverseer).
- internal/core/overseer.go: `diskCleanup` field + construction with the daemon event sink.
- internal/core/disk_cleanup_test.go: 6 tests — breach runs once per cooldown, below-watermark noop, exec error fail-open, nil receiver inert, cooldown expiry, event emitted.
- docs/discovery/orphanclaim-disk-guard-i6we.md: triage — part 1 (orphan janitor) verified already landed (Charon recoverOrphanInProgressBeads + scheduler reconcileOrphanClaim + digest grace chain); part 2 (disk cleanup) was the real gap, implemented here.
- CHANGELOG.md, docs/architecture/shepherd.md: entry + event-table row.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go build ./...` — PASS (no output).
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -run TestDiskCleanupGuard -count=1` — PASS (ok).
- `gofmt -l` on touched files — clean.
- Commit a4d40f4 verified via `git show --stat` (7 files, +224).

## Open edges for the next phase
- Cleanup scope intentionally minimal (tmpfiles only); if a real 99% event occurs, journal vacuum / cache capping may be needed — add commands to `diskCleanupCommands`.
- Disk 99% digest claim previously stale (38–43% in prior triages); guard is inert below watermark so no risk if stale.
