# discovery handoff (hydra-rsyw)

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-rsyw.md: triage of the 19-bead integration-exhausted shared wedge — both named causes (PR #389 open, disk 99%) verified fixed; disposition documented.
- CHANGELOG.md: Documentation entry under Unreleased.
- No code change (triage/discovery bead; fixes already landed upstream).

## Verification (commands run + pass/fail)
- `br show hydra-rsyw` (/opt/dev-tools/components/hydra) — OPEN→in_progress, pan-filed, root causes: PR #389 already-exists + disk 99%. PASS.
- Events `/opt/dev-tools/var/hydra/events/2026-09-29.jsonl` — 1,056 `already exists` integration failures hourly 06:00–17:00 UTC (peak 396 @15:xx, 368 @16:xx), 4 after 17:30; remaining failures are `rebase attempt 1 failed — human review needed` (fail-closed rebase cap, expected). PASS.
- `df -h /` — 334G/451G used, **78%** (below 85% watermark) → disk cause cleared. PASS.
- `grep -n 'func isPRAlreadyExists' internal/core/publisher.go` — single declaration at :300; PR-reuse fix landed via PR #418 (merge 8ad10d8) + regression tests (37807da, hydra-uqxe). PASS.
- `go build ./internal/core/` — PASS (RC=0).
- Remaining open beads carrying `integration-exhausted` in hydra store: hydra-xxq5 (sibling triage bead, open), hydra-rsyw (this), hydra-uqxe (storm triage, in_progress) — per-bead parks already cleaned by earlier triages (s402/10ka).
- `br doctor migrate-schema recover` — ran once for "database is busy (recovery in progress)" WAL poison; store readable after. PASS.

## Open edges for the next phase
- Human/batch action: clear stale `integration-exhausted` + `integration-failed` labels on parked beads whose work is not superseded (operator `POST /api/v1/projects/:name/unpark`); srv-telematics store daemon needs rebuilt binary for PR-reuse fix (`scripts/verify-deploy.sh`).
- Optional hardening (separate bead): distinct `publish_disk_full` classified event so a disk-watermark repeat shows as one cause, not N integration failures.
- beads.db WAL poison recurs under daemon concurrency — recurring, previously recorded, still open.
