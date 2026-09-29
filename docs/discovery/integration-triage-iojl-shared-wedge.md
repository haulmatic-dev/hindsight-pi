# Integration triage — shared wedge: integration-exhausted hard-parks (hydra-iojl)

Date: 2026-09-29 · Bead: hydra-iojl · Type: discovery/triage · No code change required.

## Claim under triage

Three beads (hydra-d778, hydra-9ydi, hydra-014y) hard-parked `integration-exhausted`
"within the last hour", proposing fleet-wide integration-path failure and possibly a
hydra code change to auto-rebase stranded branches before exhaustion.

## Verified findings

Measured against `origin/development` in `/opt/dev-tools/components/hydra`
(`git fetch origin development`, `git merge-tree --write-tree`):

| Branch | Commits ahead | Conflict files |
|---|---|---|
| `hydra/wt-hydra-d778` | 2 | `docs/architecture/system-loop.md` |
| `hydra/wt-hydra-9ydi` | 2 (docs-only: handoff + disposition) | `docs/hydra/handoff/task-242b.md` |
| `hydra/wt-hydra-014y` | 1 | `internal/core/service_starter.go` |

1. **Not fleet-wide, and not new.** All three parks are carry-over from the
   2026-09-29 wedge already triaged by hydra-s402 (root cause: local development
   checkout 29 commits behind origin; resolved via PR #340) and re-triaged in the
   d778/014y discovery. Each branch conflicts with `origin/development` in one
   file; each is an older iteration of work that already landed in a newer form:
   - **d778** (FR-002 spawn exclusivity) — superseded by landed FR-003
     `target_overlap.go` (84bffe1); FR-002 never landed and is not needed.
   - **014y** (stale-escalation wedge) — superseded by the landed
     EscalationTTLScanner (`internal/core/escalation_ttl.go`, default 72h) and the
     Pan `clear-escalation` repair.
   - **9ydi** — its own branch (docs-only) already contains the identical
     disposition for the bead it triaged (hydra-242b): superseded by
     escalation_ttl + clear-escalation; close, do not unpark, do not rebase.

2. **No code change needed.** The proposed "auto-rebase stranded branches before
   exhaustion" already exists on `origin/development`:
   - `internal/core/publisher.go`: `publishWithRebaseRetry` (conflict → rebase →
     retry, cap 2, fail-closed to human review).
   - `internal/core/integration_retest.go` + `overseer.integration_retest` config:
     periodic mechanical retest of parked branches, Tier B, opt-in.
   Extending this into pre-exhaustion auto-rebase would duplicate
   `publishWithRebaseRetry` semantics for marginal gain — recorded as an open
   edge, not implemented (smallest change that wins).

3. **Pipeline healthy.** `git log origin/development` shows recent PR landings
   (PR #340 merge, `hydra: integrate hydra/wt-hydra-3p41`, s402). Integration
   failures here are per-branch drift/supersession, not a broken merge queue.

## Disposition

- **Do NOT rebase** any of the three branches — rebasing would revert newer
  landed work (verified for 014y in the d778/014y discovery).
- **Do NOT clear the hard-parks** while supersession stands; the correct closure
  is human/Pan `abandon-with-summary` on d778, 9ydi, and 014y with a
  "superseded by landed work" note (s402's earlier label removal proved
  insufficient — labels re-appeared because the supersession verdict supersedes
  any retry).
- No hydra code change in this bead. `publishWithRebaseRetry` and
  `integration_retest` already cover the retry/backoff/base-refresh contract.

## Open edges

- If supersession-closure loops recur, consider a Pan `abandon-superseded` verb
  (dedupe against landed feature signature) — out of scope here.
- `integration_retest` is opt-in (`enabled: false` default); operators wanting
  automatic conflict recheck on parks must enable it in hydra.yaml.
