# Triage: orphaned in_progress claims + disk watermark (hydra-i6we)

## Part 1 — orphan janitor: ALREADY LANDED, no code change

Verified present on this branch / origin/development:
- `internal/core/charon.go`: `recoverOrphanInProgressBeads`, `orphanBeadGrace` 5m — reaps
  worker rows whose process is gone and reopens in_progress beads with no live worker
  (incl. parked projects, per fix-vn9s).
- `internal/core/scheduler.go`: `reconcileOrphanClaim` — retry-exhausted in_progress bead
  with no live registry worker and no hard-park label is reset to `open` with one fair
  retry, typed `orphan_claim_reset` event (bb75c4a).
- Digest false-alarm chain fixed in PR #70 / #106 chain: 5m claim grace, pan-filed
  exclusion, heartbeat-fresh liveness, escalated-label exclusion.
- Live behavior confirmed by prior triages (hydra-qqtl, hydra-oqp8, hydra-yusp):
  Charon emitted real `charon_orphan_reopened` events (fmsfe-sn5, 1p1q, vk94 on 2026-10-02).
Conclusion: proposal (1) is implemented and battle-tested; re-implementing would duplicate it.

## Part 2 — disk-watermark guard: GAP, implemented in this bead

- Before: `collectDiskWatermark` (internal/core/pan_health.go) reported the breach in the
  digest but only as advice ("run systemd-tmpfiles cleanup") — no automatic action.
- Now: `internal/core/disk_cleanup.go` — `diskCleanupGuard.maybeCleanup` runs
  `systemd-tmpfiles --clean` (30s timeout, no rm) once per hourly cooldown when
  used% ≥ watermark, emits typed `disk_cleanup_run` event, fail-open end to end.
- Wired from `collectHealthDigest` via `o.diskCleanup` (constructed in NewOverseer with
  the daemon EventSink).

## Note on the disk 99% claim

Prior live checks (hydra-oqp8 2026-10-02: 38%; hydra-mvcx: 42%; hydra-4yuw: 41%) showed
the 99% digest claim stale. The guard is still correct: it is a no-op below 85% and
becomes a real remediation if/when the watermark is breached.
