# Integration-exhausted shared wedge — triage (hydra-10ka)

Date: 2026-09-29 · Beads: hydra-d778, hydra-9ydi, hydra-q5ey, hydra-014y

## Symptom

Four beads hard-parked with `integration-exhausted` inside a 33-minute window,
suggesting a fleet-level integration/merge-capacity wedge. Per-bead
clear-hard-park repairs were withheld pending verification of the underlying
cause.

## Findings (all VERIFIED)

1. **Integration pipeline capacity is healthy.** `origin/development` shows
   recent PR landings flowing (PR #333, #332, #340 merged 2026-09-29). The
   wedge was not runner availability or merge serialization.
2. **Root cause: stale superseded branches.** Each parked bead's
   hydra-owned branch is an older iteration of work that already landed in
   `origin/development` via a different PR:
   - `hydra-d778` (FR-002 spawn exclusivity lock) → superseded by FR-003
     `internal/core/target_overlap.go` (84bffe1).
   - `hydra-q5ey` and `hydra-014y` (escalation TTL + clear-stale-escalation
     proposal) → implemented as `internal/core/escalation_ttl.go` (2ee7449)
     plus the Pan `clear-escalation` repair.
   - `hydra-9ydi` is a pan-filed triage bead about hydra-242b's hard-park;
     its subject is the same wedge.
3. **Conflict evidence:** `git merge-tree --write-tree origin/development
   hydra/wt-hydra-{d778,q5ey,014y}` all exit 1 (conflict), and their diffs
   delete newer code from `origin/development` — rebasing would revert
   landed work. This matches the earlier revised disposition in
   `docs/discovery/integration-triage-d778-014y.md`.
4. **The proposed code change already exists.** Time-boxed integration
   retries with backoff instead of a terminal hard-park = the
   `integration_retest` stage (`internal/core/integration_retest.go`,
   Tier A of the integration-conflict plan). It is enabled on this host
   (`/etc/hydra/hydra.yaml`: `integration_retest.enabled: true`,
   `max_attempts: 5`, `retry_interval: 1h`): each parked bead gets a
   mechanical merge-tree recheck per retry interval, capped at 5 counted
   outcomes; provably clean branches land via the dogfood PR path.
   Conflicts are deliberately never auto-resolved (Tier B, separate bead).

## Disposition

- All four beads **closed as superseded** (abandon-with-summary; live-store
  `br close` executed 2026-09-29 with per-bead rationale in the transition
  comment).
- No code change required: the retry/time-box mechanism the bead proposed
  is already shipped and enabled. Remaining gap (Tier B agentic conflict
  resolution) is tracked separately.
- Hard-parks were left in place until close; clear-hard-park was correctly
  withheld since the branches were never rebaseable.
