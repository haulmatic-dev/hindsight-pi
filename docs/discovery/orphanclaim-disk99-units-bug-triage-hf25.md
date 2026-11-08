# Triage: P0 orphan claims + publisher wedge + 99% disk (hydra-hf25)

Date: 2026-10-06 15:0x–15:3x UTC · Bead: hydra-hf25 (pan-filed, `hydra-campaign-anomaly` source)

## Verdict

- **Orphan claims: FALSE ALARM.** All 19 in_progress hydra beads had registry rows with
  fresh heartbeats (self `working`; others `completed` within the 5m grace). Escalated
  (q207, rxi6) and pan-filed meta beads excluded by design (7548196 / 7ef09f6).
  Charon live: 64 `charon_orphan_reopened` today, latest 15:05:30Z. Zero live orphans.
- **Publisher wedge: ABSENT.** Local checkout HEAD 343b7377 == origin/development,
  `git rev-list --count origin/development..HEAD` = 0. No open `dogfood/publish-hydra` PR.
- **Disk 99%: REAL BUG (now fixed).** Host filesystem is 40% used, but the digest
  reported `used=99% path=/` on every tick (448 `disk_cleanup_run` events on 2026-10-06,
  every campaign bead carried the 99% claim).

## Root cause of the 99% claim

`collectDiskWatermark` (internal/core/pan_health.go) mixed units:

```go
total := fs.Blocks * uint64(fs.Bsize)          // bytes
usedPct := int(((total - fs.Bfree) * 100) / total)  // fs.Bfree is in BLOCKS
```

`fs.Bfree` counts free blocks, not free bytes. On a 40%-used 451G ext4 volume the
formula yields `(4.8e11 − 73M)/4.8e11 ≈ 99%` on every filesystem, regardless of
actual usage. This is the true root cause behind the entire recurring "disk 99%"
false-alarm class (0ndi, 8gvw, ew7t, cgud, pf9c, i1kt, vwqu, sd9f, gzkr, lg7b, …);
all earlier triages measured df correctly but kept treating the digest number as a
stale snapshot.

## Fix

- internal/core/pan_health.go: block-based calc `(Blocks − Bfree) * 100 / Blocks`,
  with the incident comment. Regression test
  `TestCollectHealthDigestDiskUsedPctMatchesDf` pins digest DiskUsedPct within ±2 of
  the real statfs block ratio (RED against the bug: 99 vs ~38).
- Operator step: rebuild + drain-restart daemon so the fixed binary is live; until
  then the digest keeps filing disk-99% twins (~2–4/min cadence observed).

## Live-store actions

- `br doctor migrate-schema recover` — cleared recurring WAL poison.
- Closed 5 same-day twin filings (hydra-z0do, hydra-m3bj, hydra-o331, hydra-284j,
  hydra-9ufq) as `not_planned: duplicate of hydra-hf25`, triage comment each (✓ ×10).

## Verification evidence

- `python3 statvfs('/')` → 37% (bfree) / 41% (bavail); `df -h /` → 40%; `df -i` → 20%.
- Go probe with `syscall.Statfs_t` reproduced 99% under the buggy formula, ~38% under
  the fixed formula.
- Registry `workers` table cross-checked against `br list --status in_progress` (19 rows).
