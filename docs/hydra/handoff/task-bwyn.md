# Discovery handoff (hydra-bwyn)

## What changed (files + one line each)
- internal/core/publisher.go: `isPRAlreadyExists` classifier + reuse of an existing PR in `PublishViaPR` and `publishBranchViaPROnce` (create fails "already exists" → fall through to merge on the existing PR).
- internal/core/bead_closer.go: PR-first close treats PR-already-exists (publish merged the existing PR) as landed instead of a consecutive integration failure.
- internal/core/publisher_test.go: `TestIsPRAlreadyExists_Classification`, `TestPublishAfterIntegrate_PRAlreadyExists_Benign`.
- CHANGELOG.md: Fixed entry under Unreleased.
- docs/discovery/integration-triage-bwyn-duplicate-pr.md: triage report (root cause, bead list, disposition).
- docs/hydra/handoff/task-bwyn.md: this handoff.

## Verified findings (duplicate-class confirmation)
Bead hydra-bwyn is a duplicate-class finding of hydra-242b's pan-filed proposal, but the wedge ROOT CAUSE observed in the last-2-minute window (2026-09-29 14:56–15:03 UTC) is NOT the stale-escalation issue 242b described — it is a publisher bug, now fixed on this branch:
- Events `/opt/dev-tools/var/hydra/events/2026-09-29.jsonl`: every hard-park in the window fails at `pr-first close ... via dogfood/<id> → <base>: pr merge: create PR: gh pr create: exit status 1 — a pull request for branch "dogfood/<id>" into branch "<base>" already exists: <URL>`.
- Examples: hydra-ycmu → PR #366 (hydra repo); qj74 → PR #310/#329; 04jq → PR #335 (srv-telematics).
- The work was intact and a PR was OPEN; each retry failed create → counted a consecutive integration failure → cap → hard-park `integration-exhausted`. 38 distinct beads hit this class on 2026-09-29 (161 events).
- Duplicate-landing guard existed for "No commits between" but not for "already exists".

## Disposition
Code fix landed on this branch (reuse existing PR + merge). After merge+deploy, the parked beads' cause is fixed → `repair:clear-hard-park` is warranted per-bead (or integration-retest picks them up).

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS
- `go test ./internal/core/ -count=1` — PASS (1192 subtests, 0 FAIL, 53.5s)
- `go test ./internal/core/ -run 'TestIsPRAlreadyExists|TestPublishAfterIntegrate_PRAlreadyExists|TestPRConflict|TestPublishBranchViaPR_Rebase|TestPublishWithRebaseRetry' -count=1` — PASS

## Open edges for the next phase
- The parked beads carry stale `pr-open`/dogfood branches; after deploy, run integration-retest or `repair:clear-hard-park` to drain them.
- "Warning: 244/261 uncommitted changes" in the srv-telematics working tree accompanies these failures — uninvestigated, may deserve its own triage.
- `maybeOpenAutoPR` fails closed on merge failure of an existing open PR (returns false) — unchanged; merge failures there remain human-review territory.
