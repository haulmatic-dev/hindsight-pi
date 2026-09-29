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
