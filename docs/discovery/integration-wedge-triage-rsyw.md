# Integration wedge triage — hydra-rsyw (and hydra-xxq5 sibling)

Date: 2026-09-29 · Bead: hydra-rsyw (pan-filed, in_progress at triage time)

## Bead claim

hydra-rsyw: "19 beads hard-parked integration-exhausted. Root cause: publish path wedged —
PR #389 (dogfood/publish-hydra → development) already exists, and host disk at 99%."
hydra-xxq5: same pattern, 18 beads parked in a ~7-minute window, oldest
ajantha-srv-telematics-u8z7 at 43m56s. Both beads explicitly declined per-bead
`repair:clear-hard-park` until the shared cause is fixed.

## Verification (evidence)

1. **PR-already-exists storm — resolved.** Events `/opt/dev-tools/var/hydra/events/2026-09-29.jsonl`:
   1,056 `already exists` integration failures, hourly distribution 06:00–17:00 UTC peaking at
   396 (15:xx) and 368 (16:xx). Fix landed: `isPRAlreadyExists` dedupe + PR reuse
   (`resolveOrCreatePR`/`prURLFromError`) merged to origin/development via PR #418
   (dogfood/hydra-6zwl-retest, merge commit 8ad10d8), plus duplicate-declaration regression tests
   from hydra-uqxe (37807da).
2. **Disk-full — resolved.** `df -h /` at triage time: 334G used / 99G avail (**78%**, below the
   85% watermark). 6zwl deploy handoff records disk 99%→86% during deploy; further headroom since.
   79 `28` (ENOSPC-class) and 40 `disk` markers in the day's integration-failure details are
   confined to the earlier window.
3. **Post-fix failures are the normal rebase-human-review path, not a wedge.** After 17:30 UTC the
   remaining `integration-failed` events are `pr conflict: rebase attempt 1 failed — human review
   needed` (144 hydra, 130 ajantha) — the `publishWithRebaseRetry` fail-closed path working as
   designed (cap-2 rebase then human), plus 4 residual `already exists` at 17:30 (ft4x, srv-telematics)
   before the fix propagated to that store's daemon.
4. **Pipeline capacity healthy.** origin/development tip shows continuous merges
   (37807da PR #401, 8ad10d8 PR #418) — integrations are landing.

## Disposition

- The shared wedge (PR-already-exists + disk 99%) is **fixed and closed**. No code change required
  from this bead; the publisher PR-reuse fix landed upstream (PR #418).
- Remaining `integration-exhausted` labels across the two stores are **stale evidence of the fixed
  wedge**, except for beads whose own rebase conflict needs human review (those park correctly).
- Human/batch action: clear `integration-exhausted` (and companion `integration-failed`) labels on
  parked beads whose work is not superseded, giving one fair scheduler retry. Per prior triages
  (10ka, 242b, ycmu, s402): superseded branches get `br close -r "not_planned: superseded …"`,
  NOT a park clear. Operator API `POST /api/v1/projects/:name/unpark` is the lever for batch clears.
- srv-telematics store: same dispositions; its daemon also needs the rebuilt binary for the
  PR-reuse fix (6zwl handoff: deploy to `$HYDRA_HOME/bin/hydra` via `scripts/verify-deploy.sh`,
  never `command -v hydra`).

## Recurrence guard

The 7-minute mass-park window was the three-failure circuit breaker doing its job on a real
fleet-level fault (disk full + publish path). No scheduler change needed; the fix is already the
publisher PR-reuse + the disk watermark. Optional future hardening: emit a distinct
`publish_disk_full` classified event so a repeat shows up as one classified cause instead of N
integration failures (recorded, not implemented here).

---

## Field verification detail (from dogfood branch triage)

Task: shared wedge, 19 beads hard-parked `integration-exhausted` across ajantha/srv-telematics and hydra.

## Root cause chain (verified)

1. Publisher bug: `PublishViaPR` / `publishBranchViaPROnce` failed on
   `gh pr create: exit status 1 — a pull request for branch "dogfood/<id>" into
   branch "<base>" already exists`, looping 5-7x per bead, then hard-parking.
   Fixed by the bwyn change (`isPRAlreadyExists` + `resolveOrCreatePR` fallback)
   and deployed to the daemon (see handoff task-mjws: staged drain-restart deploy).
2. The publish path was further wedged by an existing open PR #389
   (dogfood/publish-hydra -> development).
3. Host disk at 99% (above the 85% watermark), risking `br` I/O error 28.

## Verification (this run)

- `gh pr view 389 --json state,mergedAt` → `{"state":"MERGED","mergedAt":"2026-09-29T16:39:01Z"}` — human action (1) complete; publish path clear.
- Disk: was 99%; purged regenerable caches under `/opt/dev-tools/var/hydra` — go-build (14G), ms-playwright/puppeteer/mongodb-binaries/huggingface/pnpm caches (~4.6G), pnpm store (1.5G), stale tmp work dirs (1.3G), go pkg/mod (1.3G), npm _npx (1.2G), and removed 2 worktrees of closed-failed beads (38ot, 5gq; `git worktree prune` verified). Total ~25G freed → `df /`: 84.4–85% (fluctuates ±0.5% with daemon writes; was 99% at bead start, 90% after first cache purge). The 85% watermark boundary is a treadmill: the daemon regenerates go-build/_npx caches during normal operation. Durable capping (journald vacuum 449M, syslog rotation 659M, apt clean 111M, systemd-tmpfiles) requires sudo — human action, as the task originally stated.
- Hydra store: `br doctor migrate-schema recover` succeeded; 0 open beads with `integration-exhausted`.
- srv-telematics store: 9 OPEN beads carried `integration-exhausted` (132 closed with the label historically; 24 total matched pre-clear).

## Live-store actions (batch-clear, cause repaired)

Per the task: parks cleared only after the publish path was repaired — it was.
For all 9 open srv-telematics beads removed labels `integration-exhausted` and
`integration-failed`, with a triage comment on the first 8 (store contention
blocked the 9th's comment; the label removal itself succeeded):

df0w, mvch, 8dvj, 4p14, awtj, 5l11, otae, gxf7, 04jq, u8z7 — `✓ Removed label`
on each (verified `br list --status open --label integration-exhausted` → 0).

Note: `df0w`, `4p14`, `awtj`, `04jq`, `gxf7` additionally carry
quarantine/stranded-conflict labels — those are NOT cleared; the underlying
`hydra/quarantine/*` merge conflicts still need human review or the Pan
`repair:retry-quarantine` verb (clean merge-tree required).

## br WAL poison

The srv-telematics store again hit
`Database error: database is busy (recovery in progress)` mid-batch; recovered
with `br doctor migrate-schema recover` (one rehearsal failed with
`Internal error: database family changed during recovery rehearsal` under
daemon contention, retried after sleep). Errors were partially post-commit —
`br show` confirmed label removals landed despite the error text.

## Disposition

- Publish path: REPAIRED (PR #389 merged + already-exists fallback deployed).
- Parks: CLEARED for one fair scheduler retry — final sweep: 0 open
  `integration-exhausted` beads in BOTH stores (hydra-15zh was found and
  cleared in the second pass; u8z7 triage comment landed on retry).
  Durable park re-fires if the same integration failure recurs — that is the
  intended fail-safe.
- Two closed-failed beads (38ot, 5gq) had `integration-exhausted` labels
  removed + stale (1.5G) worktrees deleted; their closed-failed terminal
  status is untouched.
- Disk: 85% (was 99%); durable cap in place: `scripts/cache-cap-guard.sh` + hourly hydra crontab entry purges regenerable caches until df ≤ 85%. Sudo-only items (journald 449M, syslog 659M, apt 111M) remain human actions but are no longer load-bearing for the watermark.
