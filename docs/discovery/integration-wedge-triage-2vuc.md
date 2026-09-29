# Shared wedge triage: integration-exhausted hard-parks clustering (hydra-2vuc)

Beads: hydra-xxq5 (hydra store, mass-park meta bead, 18 beads), ajantha-srv-telematics-burr
(srv-telematics store, 8-bead meta bead). Task asked: what exhausts attempts, is a human
integration/rebase needed, should retry policy back off instead of parking.

## Root cause (verified)

The mass parks were NOT integration-capacity or merge failures. Proven root causes, in order:

1. **Poisoned br WAL index** — `Database error: database is busy (recovery in progress)` on
   every concurrent reader/writer of the hydra beads store. When `br` operations fail with
   this error, the close-path integration stages record consecutive failures, which exhausts
   the cap within minutes across sibling beads → mass `integration-exhausted` hard-parks.
   Repair applied this bead: `br doctor migrate-schema recover` ×2 on
   /opt/dev-tools/components/hydra/.beads — both recovered cleanly (pre-recovery state
   retained under .br_recovery/). This matches the proven playbook rule.
2. **Stale integration base + PR-already-exists publisher loop** — prior triages (qzxv,
   movi, 8odx comments on burr; s402, bwyn, 6zwl handoffs) already fixed both:
   `ResolveIntegrationBase` (origin-default precedence, per-project `integration_branch`
   knob), `publishWithRebaseRetry`, and `resolveOrCreatePR` (PR-already-exists reuse).
3. **Superseded branches** — most parked branches are older iterations of already-landed
   work; disposition for those is abandon-with-summary, never rebase.

## Bead dispositions

- **hydra-xxq5** — mass-park meta bead (pan-filed, labels: integration-exhausted,
  integration-failed, gate-failed*). The window it reported corresponds to the WAL-poison
  outage, now recovered. No further code change warranted: retry backoff/policy already
  exists (`integration_retest` max_attempts=5 @1h; per-repo integration gate with 30s
  cooldown). Parks of superseded branches stay durable (human-only closes). Keep OPEN as
  the tracking meta bead until its constituent parks are dispositioned; or close as
  duplicate of the canonical shared-wedge bead hydra-qzxv.
- **ajantha-srv-telematics-burr** — meta bead, IN_PROGRESS with prior triage comments
  (qzxv/movi/8odx chain): underlying causes fixed, srv-telematics wt-branches merge-tree
  CLEAN post-fix, labels cleared for one fair retry; disposition on loop =
  abandon-with-summary. Awaiting live-store human/pan close; no repair from this bead.

## Retry-policy answer

Should the policy back off instead of parking? Already implemented upstream: the durable
hard-park IS the backoff — the scheduler never retries a hard-parked bead; `integration_retest`
re-checks parked branches on a 1h interval (max 5 attempts) and per-repo integration gating
serializes attempts with a 30s cooldown. No new knob needed. What remains human-only is the
repo-content merge and the abandon/close decision on superseded branches.

## Verification evidence

- `br doctor migrate-schema recover` ×2 (hydra store) — "Recovered engine read admission"
  both times; subsequent `br show hydra-xxq5` reads succeed (previously the same command
  failed with "engine recovery failed at live-recovery").
- `br show hydra-xxq5` — OPEN, mass-park description, labels intact (no label clearing
  attempted: constituent superseded parks must be dispositioned individually first).
- `br show ajantha-srv-telematics-burr` — IN_PROGRESS, closed-failed+pan-filed, three prior
  triage comments documenting cause-fixed + labels-cleared + loop=abandon.
- `go build ./...` in the hydra-2vuc worktree — PASS; `go vet ./internal/core/` — clean.
  (Bead description's gate failure, `isPRAlreadyExists redeclared in this block`, was the
  6zwl reconciliation defect; the dedupe commit e0c70a6 is in this worktree's history.)
