# Triage: "4 orphaned in_progress claims in hydra wedge the DAG ladder" (hydra-v5xq, 2026-10-06)

## Verdict

FALSE ALARM on the orphan class; the real residue is a **verified-close escalation loop**, now
drained. No reconciliation-code defect exists — both Charon and the digest behaved correctly.
No code change required.

## Evidence (all verified 2026-10-06 ~12:30 UTC)

- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 19 rows; 17 in_progress hydra beads
  cross-checked. Exactly 3 had **no live worker** (no row or non-live row):
  **hydra-k991, hydra-kvsc, hydra-ee5r** — the only orphan-claim candidates. (A transient 4th
  candidate at digest time was any of the beads Charon reopened minutes later; all currently
  have live/completed fresh-heartbeat rows.)
- All 3 candidates carry `escalated` + `needs-human` + `verified-close-escalated`:
  - k991/kvsc: `bead.escalated` tdd-validation-failed on 10-04 07:31/07:11 **and again
    10-06 11:19/11:20** — i.e. a prior re-queue looped the identical close rejection.
  - ee5r: `bead.escalated` empty-diff 3/3 on 10-06 11:38.
- Charon correctly skips escalated beads (`escalatedLabel` guard in
  `internal/core/charon.go recoverOrphanInProgressBeads` — "reopening it would respawn the
  exact death the escalation stopped"). This is fail-closed **by design**, not a miss.
- Digest: deployed binary rebuilt 2026-10-06 12:16 (includes 7548196 escalated-exclusion in
  `pan_health.go:177`). **Zero "P0 campaign anomaly" filings in 10-02 → 10-06 event files.**
- Disk: `df -h /` → 208G/451G = **49%**, below the 85% watermark — the bead's 99% claim is
  stale (same stale-claim class as prior triages 5fbq/6d01/x60p/oqp8).

## Root cause of the persisting anomaly

Not orphan recovery. The loop is: escalation_ttl / Pan clear-escalation re-queues an
**already-landed** refactor bead → worker respawns → verified-close rejects again
(tdd-validation-failed or empty-diff, since the work is already on origin/development) →
re-escalate. Force-releasing these claims would re-enter that loop; closing them as landed
is the correct release.

## Landed-ness verification (why the closes are safe)

- k991: `git grep -cE "closeDepOpenEvent|emitGateEvent" origin/development -- internal/core/`
  → 8 hits in `bead_closer_hard_gates.go`. LANDED.
- kvsc: `applyVerificationContract` → 5 hits in
  `.agents/skills/task-breakdown/runner.mjs`; handoff `docs/hydra/handoff/task-kvsc.md`
  present on origin/development. LANDED.
- ee5r: `ErrFileBeadDuplicate` → 3 hits in `internal/pan/pan.go`; persistent normalized-title
  dedupe at `internal/core/pan_health.go:439`. LANDED.

## Live-store actions executed

- Triage comment on each of k991/kvsc/ee5r (all "Comment added" ✓).
- `br close` ×3 with not_planned landed reasons (✓ ×3, after one
  `br doctor migrate-schema recover` for the recurring "database is busy (recovery in
  progress)" WAL poison, exit 6).
- Result: 0 in_progress beads without a live worker and without an escalated label remain.

## Open edges

- The clear-escalation → respawn → re-escalation loop for **landed** work is a real churn
  edge: a Pan repair (or escalation_ttl) that re-queues a bead whose delta is already
  integrated will always die at verified-close. Consider a pre-spawn "already integrated"
  check (mechanism exists: `branchMergedIntoBase` in publisher.go / zero-diff close gate)
  before honoring a re-queue of an escalated verified-close bead. Recorded, not fixed here
  (smallest change).
- br WAL poison recurred again (exit 6, cleared by `br doctor migrate-schema recover`) —
  known chronic issue, prior triages 2vuc/8py6/qqtl.
