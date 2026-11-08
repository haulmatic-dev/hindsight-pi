# Discovery: FR-002 Unverifiable-scar exit — bounded attempts, operator-visible verdict

Bead r3p60 · discovery phase · PRD: `docs/PRDs/quarantine-truth-prd-20261108.md` §FR-002.

## Current state (verified on this worktree)

- `internal/core/quarantine_reconciler.go:360` `revalidateMemberLocked`: rerun of the tainted
  member's verification command in a scratch checkout of the quarantine branch. `!ran || !passed`
  → log `ran=%v passed=%v — closed-failed stands`; label never removed. **`ran=false` has no
  exit and no counter** — a member with no declared verification command loops forever at the
  30-min `revalidationCooldown`.
- `internal/core/bead_closer.go:221` `RunVerificationGateForRevalidation` returns
  `(passed, ran bool)`; `ran=false` conflates no-command-declared and runner fault
  (`cmd, ok := verificationCommandFor(bead); !ok || c.gateRunner == nil → (false, false)`).
- `chainHasFailedBeadIDs` (`quarantine_reconciler.go:423`) returns the FIRST `closed-failed`
  carrier as `who` — no skip for stamped members.
- `QuarantinedChainEntry` (`:74-82`) has no streak field. `loadLocked` (`:176`) unmarshals the
  whole entry list; corrupt registry keeps in-memory entries (merge rule below needed).
- Operator API `clear-quarantine` (`internal/api/operator.go:35`) intersects a fixed
  `quarantineLabels` set ({stranded-work, stranded-conflict, integration-unverified,
  integration-exhausted, worktree-unavailable}) — `closed-failed` / `closed-failed-unverifiable`
  are NOT in it; operator escape is manual `br label remove`, as in the kzlu recovery.
- No `internal/core/quarantine_unverifiable_test.go` exists yet.
- FR-001 (scar scrub) is NOT implemented on this base; FR-002's streak mechanism is
  self-contained in the reconciler and does not compile-depend on FR-001 (PRD "Depends On"
  orders the chain, not the diff).

## Ambiguities resolved (for GREEN)

1. **Three-way revalidation result (AC1).** Decision: do NOT change
   `RunVerificationGateForRevalidation`'s signature. `revalidateMemberLocked` checks
   `verificationCommandFor(bead)` first: no command → no-command attempt (streak++); command
   exists → call the gate; any `ran=false` there is a runner fault/timeout → log, keep label,
   **never increment**, next cooldown retry (infra fail-open; the 90-min gate timeout already
   avoids misreading long coverage runs as faults for real runs).
2. **Persistence (AC2).** `QuarantinedChainEntry` gains
   `RanFalseStreaks map[string]int \`json:"ran_false_streaks,omitempty"\``. Persistence rides the
   existing `saveLocked()` full-entry write and JSON load — no new registry file. `loadLocked`
   merge rule: an already-in-memory entry wins over the file copy (protects a fresh in-memory
   streak from being clobbered by a stale on-disk one); entries absent from memory are adopted
   from disk. `loadLocked` must NEVER reset streaks (PRD: "a per-tick reset would make the
   streak unreachable").
3. **Threshold + stamping (AC3/AC4).** At the 3rd consecutive no-command outcome:
   `addLabels({closed-failed-unverifiable})` alongside the existing `closed-failed`, emit one
   typed `quarantine_unverifiable` event (`Bead`=member ID, `Project`=repo, fields
   `{attempts: 3, streak:<n>}`), then reset that member's streak to 0 (the label carries the
   verdict; re-stamping is guarded by label presence so a later cycle is a no-op, and the
   counter does not grow unboundedly afterward).
4. **`who` selection split.** `chainHasFailedBeadIDs` is refactored into
   `(tainted bool, selectable string)` semantics: `selectable` = first `closed-failed` carrier
   WITHOUT `closed-failed-unverifiable`; `tainted` = any `closed-failed` carrier (stamped
   members still hold the quarantine — AC5 fail-closed). Call sites at `:269/273/274` updated.
   Documented cost: N tainted members serialize at 30 min each via `LastRevalidation` — by design.
5. **Per-tick sweep (AC2).** In `reconcileEntryLocked` (before the re-validation branch), for
   every member whose current labels no longer contain `closed-failed`, set
   `RanFalseStreaks[id]=0` (delete-on-write). This is the only reset path besides
   verified-green (scrub, FR-001) / verified-red (`ran=true` → streak 0) and operator removal.
6. **Operator label removal (AC5).** No API change: the operator removes the labels manually
   (`br label remove <id> closed-failed closed-failed-unverifiable`); the per-tick sweep resets
   the streak, the member drops out of the tainted set, and the next reconciler tick
   re-integrates through `IntegrateBranch` (FR-003 gates that path with the suite gate).
   Documented exactly in the PRD's escape hatch.
7. **Empty data_models in bead metadata.** No new data model: member = `types.Bead` (ID,
   Labels), entry = `QuarantinedChainEntry` (schema above). The event payload is
   `Event{Type, Project, Bead, Fields}` per the existing `EmitRepo` contract.
8. **Label-op error handling.** Fail-open, matching the existing `revalidateMemberLocked`
   pattern: on `addLabels`/`removeLabels` error the streak is NOT advanced and stamping is
   retried next cooldown — an infrastructure label fault must not count toward a verdict
   about the bead's verifiability.

## GREEN checklist (contract — the discovery doc's obligation to the next phase)

- Constants: `closedFailedUnverifiableLabel = "closed-failed-unverifiable"`,
  `quarantineUnverifiableEventType = "quarantine_unverifiable"`,
  `unverifiableStreakCap = 3`.
- `QuarantinedChainEntry.RanFalseStreaks` field + load/save round-trip + merge rule.
- `revalidateMemberLocked`: command check → three-way branch (no-command++ / fault=retry /
  green-revalidate with streak reset).
- `chainHasFailedBeadIDs` split (selectable vs tainted) + call-site updates.
- Per-tick sweep reset in `reconcileEntryLocked`.
- `revalidateMemberLocked` green path resets `RanFalseStreaks[id]` (and never touches
  `verified-close-*` / `escalated` / `needs-human` labels).
- Tests in `internal/core/quarantine_unverifiable_test.go`: streak increments only on
  no-command; runner fault never increments; 3rd no-command stamps + one event + skip in `who`;
  stamped member keeps quarantine (tainted-any); restart round-trip preserves streaks;
  load-merge does not clobber in-memory streaks; label removal → sweep resets → member no
  longer tainted → re-integrate path taken; registered members re-stamp idempotently.

## AC mapping

| AC | Resolution |
|----|------------|
| AC1 runner faults never increment | A1 (command-exists check before gate; `ran=false`+cmd = fault → retry, no increment) |
| AC2 3-strike stamp + event + who skip | A3 + A4 |
| AC3 survives restart, per-tick sweep reset | A2 + A5 |
| AC4 quarantine holds; label removal resets & re-enables | A4 tainted-any split + A6 |
