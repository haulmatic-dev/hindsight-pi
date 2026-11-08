# Handoff — task-obvaq (RED)

- **Bead:** obvaq — RED: tests for Unverifiable-scar exit (bounded attempts, operator-visible verdict)
- **Phase:** red
- **PRD:** quarantine-truth, FR-002 (contract decisions: `docs/discovery/unverifiable-scar-exit-fr002-r3p60.md`)

## What landed

| File | Type | Notes |
|---|---|---|
| `internal/core/quarantine_unverifiable_test.go` | NEW (test) | 6 contract tests, all gated on `unverifiableImplemented` |
| `internal/core/quarantine_unverifiable.go` | NEW (RED stub) | Inert surface: `closedFailedUnverifiableLabel`, `quarantineUnverifiableEventType`, `unverifiableStreakMax=3`, `RevalidateVerdict{Passed,Ran,NoCommand}`, `SetRevalidationClassified`, `noteRevalidateOutcome`, `sweepUnverifiableStreaks`, `revalidateTargetFor`, `unverifiableImplemented=false`, `requireUnverifiableImpl` |
| `internal/core/quarantine_reconciler.go` | RED seam (inert) | `RanFalseStreaks map[string]int json:"ran_false_streaks,omitempty"` on `QuarantinedChainEntry`; `classifiedGate/classifiedRemove/classifiedAdd` fields on the reconciler; added `"errors"` import (see Conflicts) |

## Tests → parent ACs

1. `TestUnverifiableStreak_OnlyNoCommandIncrements` — runner faults and real red runs never increment; only no-command does (AC1).
2. `TestUnverifiableStreak_ThirdNoCommandStampsAndEmits` — 3 consecutive no-command outcomes stamp `closed-failed-unverifiable` + exactly one `quarantine_unverifiable` event (Bead=member); stamp resets the streak (AC2).
3. `TestUnverifiable_ThreeTicksStampAndWhoSkipsMember` — full `Reconcile()` wiring: stamping tick, quarantine branch holds (fail-closed), then `who` selection targets `member-2` (stamped member skipped) (AC2+AC4 hold-half).
4. `TestUnverifiable_StreakSurvivesRestartViaRegistry` — streak=2 persists to `quarantined-chains.json`; new reconciler over the same path: third outcome stamps (AC3, restart half).
5. `TestUnverifiable_SweepResetsStreakWhenLabelGone` — `closed-failed` scrubbed → per-tick sweep resets the streak to 0 (AC3, sweep half).
6. `TestUnverifiable_OperatorRemovalResetsAndReEnables` — operator removes `closed-failed-unverifiable` → next tick re-verdicts the member (gate re-invoked, target = member-1) (AC4).

## RED gate (established pattern)

- Default: `go test ./internal/core/ -run TestUnverifiable -count=1` → **ok**, all 6 SKIP loudly.
- Gated: `UNVERIFIABLE_ENFORCE=1 go test ./internal/core/ -run TestUnverifiable -count=1` → **FAIL** (all 6): `RED: unverifiable-scar exit contract not yet implemented (unverifiableImplemented=false)`. Genuine RED: feature strings had zero `rg` hits before this bead; no tests existed.

## GREEN obligations (contract, in order)

1. `revalidateMemberLocked`: use `classifiedGate` when set; cooldown bounds only real reruns (`Ran==true`) — **no-command outcomes must re-evaluate every tick** (else the streak can never reach 3; test 3 depends on this).
2. Feed every classified outcome through `noteRevalidateOutcome(e, who, verdict)`; on the 3rd consecutive no-command: add `closed-failed-unverifiable` (never remove `closed-failed`), emit one `quarantine_unverifiable` event (Bead=member, Reason, Fields), reset the member's streak.
3. `who` selection: skip members carrying `closed-failed-unverifiable` (`revalidateTargetFor` helper exists; wire into `chainHasFailedBeadIDs` path).
4. Persist `RanFalseStreaks` via the existing registry save/load (map already has the JSON tag).
5. `sweepUnverifiableStreaks(e, beadLabels)` per tick in `reconcileEntryLocked`: drop streaks for members no longer carrying `closed-failed` or `closed-failed-unverifiable` (operator removal).
6. Flip `unverifiableImplemented=true`, remove nothing else; keep nil-`classifiedGate` falling back to the legacy `(passed, ran)` gate.

## Verification (this bead)

- `go build ./...` → OK
- `go test ./internal/core/ -count=1` → **ok, 83s** (whole package green with the stub in place)
- `go vet`/gofmt clean on touched files
- Coverage: `go test -run TestUnverifiable -coverprofile` → package 0.1% (tests intentionally skip-loud pre-GREEN); `quarantine_unverifiable.go`: `requireUnverifiableImpl` 66.7%, rest 0.0% (skipped bodies). Real coverage of the counter/stamp/sweep paths lands with GREEN when `unverifiableImplemented=true`.

## Conflicts / notes for the next worker

- **Shared worktree churn:** an unrelated strand (suite-gate-in-IntegrateBranch) landed mid-bead edits to `quarantine_reconciler.go` (the `ErrQuarantineSuiteGateBlocked` block in `reconcileEntryLocked` + CHANGELOG entry). My inert seam edits coexist with them; the missing `"errors"` import caused a broken build window — I added it (single line). If the suite-gate strand's `errors` import now makes mine redundant, keep one.
- Untracked new files were observed to disappear once early in the bead (files written via a tool were gone seconds later); they were re-created via direct writes and verified with `wc -l`/`gofmt` — verify presence before continuing: `ls internal/core/quarantine_unverifiable*.go`.
- Registry fixture note: `NewQuarantineReconciler(regPath, ...)` + `Record(...)` then `Reconcile()` persists; `registryStreak` in the test reads the file directly.

## HYDRA_VERDICT

```
HYDRA_VERDICT: COMPLETE_RED
bead: obvaq
tests: 6 contract tests in internal/core/quarantine_unverifiable_test.go
default: SKIP (loudly) — go test ./internal/core/ ok, 83s full package
gated: UNVERIFIABLE_ENFORCE=1 → FAIL with "RED: unverifiable-scar exit contract not yet implemented"
coverage: stub surface 0% (pre-GREEN), requireUnverifiableImpl 66.7%; GREEN must report real coverage
files: internal/core/quarantine_unverifiable_test.go, internal/core/quarantine_unverifiable.go, internal/core/quarantine_reconciler.go (inert seams), CHANGELOG.md
no_implementation: verified (all stub bodies inert; feature strings only exist as constants)
```
