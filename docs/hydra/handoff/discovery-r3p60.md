# discovery handoff (r3p60)

## What changed (files + one line each)
- docs/discovery/unverifiable-scar-exit-fr002-r3p60.md: FR-002 discovery — verified current revalidation surface (`revalidateMemberLocked` runs a scratch-checkout gate rerun where `ran=false` loops forever with no counter), resolved 8 ambiguities (three-way result via caller-side `verificationCommandFor` check — closer signature unchanged; `RanFalseStreaks map[string]int` on `QuarantinedChainEntry` persisted through existing `saveLocked`/`loadLocked` with in-memory-wins merge; 3-strike → `closed-failed-unverifiable` + one `quarantine_unverifiable` event + streak reset with idempotent re-stamp guard; `chainHasFailedBeadIDs` split into selectable-vs-tainted so stamped members keep quarantine but stop consuming ticks; per-tick sweep in `reconcileEntryLocked` resets streaks for members no longer carrying `closed-failed`; operator removal documented as manual `br label remove` closed-failed + closed-failed-unverifiable; label-op errors fail-open and never advance the streak), AC mapping table + GREEN checklist (constants, call sites :269/273/274, test list for `internal/core/quarantine_unverifiable_test.go`).
- CHANGELOG.md: Documentation entry under Unreleased.

## Verification (commands run + pass/fail)
- `graphify query "Unverifiable-scar exit bounded attempts operator-visible verdict closed-failed-unverifiable RanFalseStreaks" --budget 2000` — ran (graph present, 242 nodes; confirmed PRD FR-002 node + current code anchors). PASS.
- Code inspection verified: `RunVerificationGateForRevalidation` (bead_closer.go:221) conflates no-command and fault in `ran=false`; `chainHasFailedBeadIDs` (quarantine_reconciler.go:423) returns first closed-failed carrier with no stamped skip; `QuarantinedChainEntry` (quarantine_reconciler.go:74-82) has no streak field; operator `clear-quarantine` label set (operator.go:35) excludes closed-failed; `revalidationCooldown` 30m (quarantine_reconciler.go:58).
- No source change (discovery bead). `internal/core/quarantine_unverifiable_test.go` does not exist yet (GREEN's file).

## Open edges for the next phase
- GREEN (RED test bead first per chain convention): implement per the GREEN checklist; PRD Test Command `go test ./internal/core/ -run 'UnverifiableScar' -count=1 -v`.
- FR-001 (scar scrub) is NOT implemented on this base; FR-002's mechanism is self-contained in the reconciler (no compile dependency) — the PRD's `Depends On: FR-001` orders the chain, not the diff.
- `loadLocked` merge rule (in-memory entry wins) is the one persistence subtlety — do not simply overwrite entries from the file.
