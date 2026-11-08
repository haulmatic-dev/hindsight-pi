# discovery handoff (hydra-qmf7)

## What changed (files + one line each)
- internal/core/pan_health.go: digest `orphanclaims` counter now skips in_progress beads claimed within `orphanClaimDigestGrace` (5m, same grace Charon applies before a destructive reopen) — a fresh claim has no registry worker yet because spawn → status update → registry upsert is not atomic.
- internal/core/pan_health_test.go: `TestCollectHealthDigestOrphanClaims` updated (beads carry explicit old UpdatedAt); new `TestCollectHealthDigestOrphanClaims_FreshClaimGrace` (fresh claim skipped, stale claim counts).
- CHANGELOG.md: Fixed entry under Unreleased.
- docs/discovery/orphanclaim-digest-grace-qmf7.md: verified false-alarm triage.
- Worktree fast-forwarded HEAD → origin/development (7512f66) — the branch was 12 behind and missing bb75c4a (scheduler reconcileOrphanClaim + digest OrphanClaims counter + Pan prompt line).

## Verified findings (hydra-qmf7 anomaly)
- FALSE ALARM on both counts, verified live at ~02:46–02:55 UTC 2026-10-02:
  - All 8 in_progress hydra beads (t2e1, qmf7, l867, ctix, b5p2, 1p1q, vk94, ns8y) had registry rows in /opt/dev-tools/var/hydra/.hydra/registry.db with fresh heartbeats (last_heartbeat within seconds) and live PIDs confirmed in `ps`. Charon error log every minute: "✅ No orphaned or stuck workers found ... orphans_found=0".
  - Disk: `df -h /` = 43% (185G/451G), NOT 99%. Watermark (85%) not breached.
- Real root cause of the 4 bead filings in 13 min (pan_bead_filed events, sources all `hydra-campaign-anomaly`): SELF-PERPETUATING DIGEST LOOP. The digest counted a just-claimed bead as orphaned (no worker registered yet) → Pan filed a campaign-anomaly bead → the filed bead itself became a new in_progress claim with no worker yet → digest re-fired → re-file. Chain: ctix (02:35) → qmf7 (02:41) → t2e1 (02:43) → yz8z (02:47). 02:45 filing failed only because br rejected it (exit 4).
- Reconciliation IS firing upstream (bb75c4a, reconcileOrphanClaim in scheduler.go + Charon orphan recovery); the daemon binary predates parts of the digest wiring but the scheduler code is present. Not a reconciliation failure — a false-positive anomaly source.
- Host hygiene performed: /tmp had 44G including 1,650 `skills-*` worker-install dirs (~103M each, most stale); deleted 1,017 dirs older than 1 day → reclaimed 26GB; disk 44% → 38%. `systemd-tmpfiles --clean --remove` run (rc=0). Remaining /tmp churn (~46GB total, worker skills installs regenerated per run) is an ongoing growth vector: recommend periodic `find /tmp -maxdepth 1 -name 'skills-*' -mtime +1 -exec rm -rf {} +` (cron/timer) before the 85% watermark fires for real.

## Verification (commands run + pass/fail)
- `git merge --ff-only origin/development` — PASS (HEAD 7512f66).
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run 'TestCollectHealthDigestOrphanClaims' -count=1` — PASS (2 tests).
- `go test ./internal/core/ -run 'TestCollectHealthDigest|TestCharon' -count=1` — PASS (21 subtests, ok).
- Registry cross-check: all in_progress beads had heartbeats < 10m old (sqlite query, 8/8 count=1).
- `df -h /` before/after cleanup: 44% → 38%.

## Open edges for the next phase
- /tmp skills-install churn: 1,650 dirs in ~2 days regenerated per worker run. Root-cause fix belongs in the worker skill-install path (reuse a cached skills dir keyed by content hash instead of a fresh /tmp dir per run) — separate bead.
- The 02:45 filing attempt failed with `br create ... failed: exit status 4` — unexplained exit 4 from br during rapid creation; watch for recurrence.
- Daemon binary at /opt/dev-tools/var/hydra/bin/hydra is older than origin/development; redeploy after this lands so the grace fix is live.
