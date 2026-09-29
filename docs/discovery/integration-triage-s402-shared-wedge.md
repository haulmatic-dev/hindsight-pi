# Integration triage: hydra-d778 / hydra-q5ey / hydra-014y shared wedge (2026-09-29)

## Root cause

Shared wedge across all three beads: the local development checkout
(`/opt/dev-tools/components/hydra`) was 29 commits behind
`origin/development`. The daemon integrates worktree branches against the
LOCAL base, so merge-base checks used stale trees and integration failed
while the remote pipeline was healthy (`origin/development` was landing PRs
normally, e.g. #337, #338).

Per-bead merge-tree conflicts (vs origin tip):

- **hydra-d778** (FR-002 spawn exclusivity lock): 1 conflict,
  `docs/architecture/system-loop.md`. Branch superseded — FR-003
  target-scope overlap serialization (`84bffe1`, `internal/core/target_overlap.go`)
  landed on origin addressing the same problem. Disposition:
  abandon-with-summary, no rebase.
- **hydra-q5ey** (stale human escalations): conflicts in
  `internal/core/escalation_ttl_test.go` (add/add),
  `internal/core/service_starter.go`, `internal/pan/pan.go`,
  `docs/architecture/shepherd.md`. Proposal superseded — escalation TTL
  already landed via hydra-la4q (`2ee7449`, plus `c3f3500` auto-expire,
  `overseer.escalation_expiry_hours` default 48h). Disposition:
  abandon-with-summary.
- **hydra-014y** (duplicate of q5ey, in_progress + gate-failed labels):
  conflict in `internal/core/service_starter.go` only. Same supersession.

Prior triage on origin (`32c5fe2`, `fdd6728`) reached the same supersession
conclusion for d778/014y but did not repair the stale local checkout, so the
parks persisted.

## Repairs applied

1. beads DB (hydra store) stuck in WAL-index poison ("database is busy
   (recovery in progress)") — fixed with `br doctor migrate-schema recover`
   (same fault also found and fixed on the srv-telematics store).
2. Local checkout fast-forwarded to origin tip: merged `origin/development`
   into local `development` (clean, `go build ./...` OK, degrade tests green),
   landed via `dogfood/s402-integration-repair` → **PR #340 (MERGED,
   squash-admin)** — development is branch-protected, direct push declined
   (`remote rejected ... repository rule violations`).
3. Cleared `integration-exhausted` + `integration-failed` labels on all three
   beads; triage comment added to each (br label remove / comment add
   verified ✓).

## Outcome

Scheduler can re-evaluate all three beads. d778/q5ey should be
abandoned-with-summary (work already landed via successor implementations);
014y is in_progress with gate-failed labels and will be re-scheduled.

## Verification

- `git merge-tree --write-tree HEAD origin/development` on the merged local
  checkout: rc=0 (no conflicts).
- `go build ./...`: PASS. `go test ./internal/degrade/ -count=1`: ok.
- `gh pr view 340`: `MERGED 2026-09-29T12:39:35Z`.
- `br label remove` output: ✓ for all three beads.
