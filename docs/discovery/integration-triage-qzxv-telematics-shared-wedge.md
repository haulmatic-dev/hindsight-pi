# Triage: mass integration-exhausted hard-parks in ajantha-srv-telematics (bead hydra-qzxv)

Date: 2026-09-29 · Scope: ajantha-srv-telematics-{hig8,i6zt,5l11,38ot,f10l,dkqc,burr,otae,nygw,kher,u8z7} + hydra-242b

## Root cause (verified)

Same wedge already documented in hydra-0e85 (closed, fix landed): the telematics main
checkout was parked on stale local branch `feat/intergration-test-plan` (261 staged
deletions present at triage time). Integration base was derived from the checkout's
current branch, so every `gh pr create` / merge-base check targeted the wrong base →
consecutive integration failures → `integration-exhausted` hard-parks across sibling
beads within minutes of each other. Not per-bead faults.

Fixes already landed upstream (verified on origin/development):
- `ResolveIntegrationBase` (internal/core/integration_base.go) — origin-default
  precedence, never the checkout branch (hydra-0e85 chain, PR #340).
- `publishWithRebaseRetry` + `integration_retest` stage (time-boxed retries).
- origin/HEAD now resolves to `refs/remotes/origin/development`.

## Pipeline health (verified 2026-09-29)

- `git merge-tree --write-tree origin/development hydra/wt-ajantha-srv-telematics-<b>`
  for all 11 branches: **exit 0, zero conflicts**, 23–58 commits ahead.
- Recent PR landings on origin/development: #324, #325 (2026-09-29).
- No open dogfood PRs pending review.

## Disposition

Parks are STALE — integration no longer conflicts. Actions taken (live store,
/home/ajantha/projects/srv-telematics, all verified via br output):

- Removed labels `integration-exhausted` + `integration-failed` from all 11 beads (✓ ×22).
- Triage comment added to each bead (✓ ×11): root cause, clean merge-tree evidence,
  one fair scheduler retry, abandon-with-summary on loop.

Per-bead notes:
- hig8/i6zt/5l11/dkqc/burr/nygw are pan-filed meta beads tracking the wedge itself;
  they can be closed (wedge-resolved) once the retry pass confirms pipeline health.
- 38ot/f10l/otae are failed-closure auto-fix beads; kher/u8z7 already closed-failed.
- hydra-242b: already closed as superseded (see task-ng18 handoff); no action.

## Hydra improvement recommendation (from bead proposal)

Fleet-wide circuit breaker: when ≥ N beads in one project hard-park
integration-exhausted inside a short window, park the project with a single
`integration-wedge` label instead of per-bead durable parks, and auto-file one meta
bead. Prior art in-tree: `internal/degrade` (FR-032) provides the windowed
attempt/success counters; `internal/core/integration_retest.go` the retry stage.
Not implemented in this bead (discovery/triage scope).

## Contraindications honored

- No rebase / cherry-pick of any parked branch (merge-tree already clean; nothing to resolve).
- `gh pr create "already exists"` failures must be treated as PR-reuse, not new exhaustion.
