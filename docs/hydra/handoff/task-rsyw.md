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

---

## Additional detail (dogfood branch version)

# discovery handoff

## What changed (files + one line each)
- internal/core/publisher.go: gate-fix — lint gate failed with `isPRAlreadyExists redeclared in this block (:671 / :300)`: this branch's integration base predated origin/development's ecc6cb5 zpyj collision fix, so integration re-introduced the duplicate decl + fake-success swallow. Resolution: took origin/development's publisher.go + publisher_test.go wholesale (fail-closed resolveOrCreatePR → resolveExistingPR → merge contract; publisher file byte-identical to upstream).
- docs/discovery/integration-wedge-triage-rsyw.md: triage of the shared integration-exhausted wedge (19 beads) — root cause verified repaired, batch-clear executed, evidence recorded.
- scripts/cache-cap-guard.sh: hourly disk-watermark guard — purges regenerable caches (go-build, _npx, pnpm, playwright, pkg/mod, stale tmp dirs, closed-bead worktrees) oldest-first until df ≤ 85%; installed via hydra-user crontab (`17 * * * *`), no sudo.
- CHANGELOG.md: Unreleased/Documentation entries recording the batch-clear + cache cap guard.
- docs/hydra/handoff/task-rsyw.md: this handoff.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` + `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run 'TestIsPRAlreadyExists|TestPublishAfterIntegrate_PRAlreadyExists|TestDefaultAutoPRGhCreate|TestAutoPRGhMerge_AlreadyMergedRace' -count=1 -v` — 6 tests PASS (incl. NoPR and ClosedPR fail-closed cases).
- `go test ./internal/core/ -run 'TestPublish|TestPRConflict|TestPublishBranchViaPR|TestMaybeOpenAutoPR|TestAutoPR|PrFirst|PRFirst|TestDefaultRebaseBranchOntoBase' -count=1 -v` — 30 PASS / 0 FAIL.
- `gofmt -l internal/core/publisher.go` — clean.
- `gh pr view 389 --repo haulmatic-dev/hydra --json state,mergedAt` → MERGED 2026-09-29T16:39:01Z — human action (1) done. PASS.
- `sh -n scripts/cache-cap-guard.sh` → SYNTAX_OK; manual run rc=0, df stays at 85%; `crontab -l` shows hourly entry. PASS.
- `df /` after full regenerable-cache purge (~25G: go-build 14G, ms-playwright/puppeteer/mongodb-binaries/huggingface/pnpm 4.6G, pnpm store 1.5G, stale tmp dirs 1.3G, go pkg/mod 1.3G, npm _npx 1.2G, 2 closed-bead worktrees 1.5G): 85% (was 99% at bead start) — br I/O error 28 risk gone; watermark drift now held by the hourly guard. PASS.
- `br list --status open --label integration-exhausted` final sweep: hydra store 0, srv-telematics 0 (hydra-15zh found + cleared in second pass; both labels + comment ✓). PASS.
- u8z7 triage comment landed on retry (verified in `br show` output). PASS.
- Triage comment added to all cleared beads.
- `br list --status open --label integration-exhausted` in hydra store (post `br doctor migrate-schema recover`): 0. PASS.
- srv-telematics: removed `integration-exhausted` + `integration-failed` from all 9 open beads (df0w, mvch, 8dvj, 4p14, awtj, 5l11, otae, gxf7, 04jq, u8z7) — all ✓; post-check `br list --status open --label integration-exhausted` → 0. PASS.
- Triage comment added to 8 of 9 (u8z7 comment skipped under store contention; labels landed).

## Notes
- srv-telematics store WAL poison recurred mid-batch; recovered with `br doctor migrate-schema recover`; several error texts were post-commit (label removal verified via `br show`).
- Quarantine/stranded-conflict labels left standing on df0w/4p14/awtj/04jq/gxf7 — merge conflicts still need human review or Pan `repair:retry-quarantine` (clean merge-tree gate).

## Open edges for the next phase
- Publisher file now byte-identical to origin/development's ecc6cb5 — future integrations from stale bases may re-collide; CASS rule saved (take upstream's newer file wholesale when merge-base predates a landed fix).
- Sudo-only residue (journald 449M, syslog rotation 659M, apt clean 111M) available as extra headroom — optional human action, not load-bearing for the watermark now that the guard runs hourly.
- cache-cap-guard.sh Tier 3 (closed-bead worktrees) calls `br show` per bead; if a store is mid-WAL-poison it fails open (no purge) — acceptable.
- If any cleared bead re-fails integration identically, the durable park re-fires — do not loop clear-hard-park; treat as recurrence signal.
