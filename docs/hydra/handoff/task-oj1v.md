# green handoff

## What changed (files + one line each)
- internal/core/pan_health.go: digest OrphanClaims loop now skips beads carrying the `pan-filed` label (after the existing escalated skip) — pan-filed triage/campaign filings are in_progress claims with no worker by nature, and counting them re-fired the P0 campaign-anomaly for the anomaly's own filings (hydra 2026-10-06: 9 twin filings 12:23–12:47Z in 25 min).
- internal/core/charon.go: `recoverOrphanInProgressBeads` now skips pan-filed beads before the destructive reopen — Charon had reopened pan-filed pseudo-anomaly filings (charon_orphan_reopened hydra-2a3f at 11:42:43Z), respawning triage tasks in a loop.
- internal/core/bead_closer.go: new `panFiledLabel = "pan-filed"` constant (kept in sync with `pan.FiledLabel`).
- internal/core/pan_health_test.go: TestCollectHealthDigestPanFiledBeadNotOrphan.
- internal/core/charon_orphan_test.go: TestRecoverOrphanInProgressBeads_SkipsPanFiledLabel.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS. `go vet ./internal/core/` — PASS. `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run 'TestCollectHealthDigest|TestRecoverOrphanInProgressBeads|TestCharon' -count=1` — PASS (21 tests, incl. 2 new).
- Live evidence (bead start): registry.db — all 21 in_progress hydra beads had either live `working` rows (4, heartbeats fresh, PIDs alive via ps) or completed rows <25 min old → zero true orphans at triage time. Charon reaped 44 charon_orphan_reopened today (recovery operating). pan_bead_filed events fired every ~2 min (source `hydra-campaign-anomaly`) — the persisting anomaly is the self-filing loop, now fixed.
- Disk: `df -h /` → 209G/451G = 49% — the bead's 99% disk claim stale; no operator cleanup required (disk_cleanup guard also live in daemon).

## Open edges for the next phase
- Deployed daemon (bin built 12:16:29Z, started 12:16:28Z) predates this fix — operator restart needed to stop the residual filing loop.
- Pan-filed beads whose worker completed still sit in_progress until closed; consider closing superseded twins (oqp8/qqtl disposition) — out of scope here.
