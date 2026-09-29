# Integration triage — hydra-bwyn duplicate-PR wedge (2026-09-29)

## Scope
Bead hydra-bwyn (pan-filed, duplicate-class finding): "multiple beads hard-parked
integration-exhausted within the last 2 minutes", cross-referenced against
hydra-242b's pan-filed proposal. Human triage requested.

## Evidence
- `/opt/dev-tools/var/hydra/events/2026-09-29.jsonl`: 161 `integration-exhausted`
  events across 38 distinct beads on 2026-09-29; the task's "last 2 minutes"
  window (14:56–15:03 UTC) covers hydra-242b, hydra-ycmu, hydra-9ydi and
  telematics qj74 / otae / 0po4 / f10l / dkqc / 38ot / 04jq / burr.
- Every failure in the window shares one error class, verbatim:

  ```
  pr-first close <bead> via dogfood/<bead> → <base>: pr merge: create PR:
  gh pr create: exit status 1
  a pull request for branch "dogfood/<bead>" into branch "<base>" already exists:
  https://github.com/<org>/<repo>/pull/<n>
  ```

- Examples: hydra-ycmu → hydra PR #366 (into `main`); qj74 → srv-telematics
  PR #310 / #329 (into `feat/intergration-test-plan`, then `development`);
  04jq → PR #335.

## Root cause
`publishSingletonViaPR` / `publishAfterIntegrate` / `PublishBranchViaPR`
treated `gh pr create` "already exists" as a real integration failure. The
existing PR — the landing vehicle — was open and mergeable, but each close
attempt counted a consecutive integration failure (`consecutive_failures_capped`)
until the bead hard-parked `integration-exhausted`. A duplicate-landing guard
existed for the "No commits between" phrasing (hydra-kf2t) but not for
"already exists". This is a fleet-level publisher bug, not the stale-escalation
wedge hydra-242b described; hydra-bwyn is therefore a duplicate-class finding
of 242b only in symptom, not in cause.

## Fix (this branch)
`internal/core/publisher.go`: `isPRAlreadyExists(err)` matches the gh wording;
`PublishViaPR` and `publishBranchViaPROnce` fall through to
`gh pr merge <head> --merge --admin` on the existing PR instead of failing.
`internal/core/bead_closer.go`: the PR-first close treats the reuse as landed
("landed via existing PR") without advancing the failure cap.

## Disposition (human-triage answer)
1. Land this fix; deploy the daemon.
2. Then drain the parked beads via integration-retest or
   `repair:clear-hard-park` per bead — the named cause is fixed by the deploy,
   which is exactly the clear-hard-park precondition.
3. hydra-242b's own disposition (superseded by escalation_ttl; do NOT clear
   its park before removing the conflicting `escalation_expiry_hours` work)
   still stands — see `docs/hydra/handoff/task-242b.md`.
4. Separate observation, not fixed here: srv-telematics failures log
   "Warning: 244/261 uncommitted changes" in the working tree — needs its own
   triage.
