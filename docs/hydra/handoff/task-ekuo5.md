# Handoff — task-ekuo5 (GREEN: completion-time scar scrub, FR-001)

Phase: green · Bead suffix: ekuo5 · Worktree: hydra-1vxr0 · Date: 2026-11-08

## What landed

`internal/core/quarantine_scar.go` implemented (RED stub replaced):

1. **AC1 — integration through the gated path.** `run()` calls the taints
   oracle (`chainHasFailedBeadIDs` in production); a stale `closed-failed`
   taint triggers the tainted member's re-verification in the chain worktree
   (`RunVerificationGateForRevalidation`). Green → `closed-failed` removed,
   `closed-failed-revalidated` added, one `label_revalidated` event with
   reason `completion_scrub`, then integrate through the gated path
   (`IntegrateBranchAndRemove`, suite gate inside). No-command members fall
   back to the shared suite gate + all-foreign-failing-package attribution
   (`scarFallbackForeign`: failing-pkg dirs vs worktree diff, fail-closed).
2. **AC2 — junk-safe pre-verdict commit.** Dirty paths (git porcelain)
   filtered by `junksig.IsJunkPath`, remainder committed with
   `git commit --only -- <paths>` before any verdict; commit failure skips
   verification entirely and quarantines.
3. **AC3 — panic/timeout fail-open.** `scarMarkerGuarded` sets the sync.Map
   marker, defer-clears it, recovers panics into `scarQuarantined` (+ optional
   `scarScrubPanicFallback` production quarantine). Wall-clock cap
   `scarScrubWallClockCap` = 3h, checked after verification.
4. **AC4 — post-scrub choke-gate failure.** `restampQuarantine` re-stamps
   `closed-failed` + `closed-failed-suite` in one `addLabels` call and emits
   `scar_scrub_failed` (reason `suite_gate_post_scrub`) BEFORE the decision is
   recorded; integrate hook count stays 0.
5. **AC5 — restart recovery.** `MarkScrubInProgress` persists the registry
   row with Status `scrub-in-progress` (initializer stamps it before launch);
   `reconcileEntryLocked` hands such rows to `recoverScrubInProgress`
   (async in production, sync for tests): re-runs the decision when the
   chain worktree branch (hydra/wt-*) exists, drops the entry otherwise.

Launch path: `launchScarScrub` stamps the marker synchronously then runs the
scrub in a goroutine (callback returns immediately). Unwired hooks (no
`wireScarScrubHooks`) fail closed to `scarLegacyQuarantine` — the pre-scrub
immediate-quarantine behavior — so a failed chain can never silently
integrate. `scarAfterScrub` integrates-or-quarantines a `scarSkipped` scrub.

Production wiring: `wireScarScrubHooks(o)` in `service_starter.go` (hooks:
taints/verify/dirty/commit/suite/fallback/integrate/quarantine/labels/
branchExists), called where `SetRevalidation` is wired.

Initialization call site: `chain_completion_initializer.go` — the
failed-beads branch now stamps scrub-in-progress + `launchScarScrub` instead
of the inline quarantine (legacy behavior preserved verbatim in
`scarLegacyQuarantine` for the unwired case).

## Deliberate decisions

- **Shared hook set.** `defaultScrubHooks()` returns a package-level
  singleton the daemon wires once and tests mutate (fixture pattern in
  `newScarFixture`). This is what lets `launchScarScrub(nil, …)` in the
  launch test observe fixture hooks without an extra seam.
- **fallbackForeign probe context.** The seam takes no arguments, so the
  production `suiteGate` closure remembers the last probe
  (`scarProbeLast`, mutex-guarded). Two chains scrubbing concurrently could
  cross-read probes; wrong = fail-closed (verdict stands). Declared-command
  path dominates in production. Recorded for later hardening.
- **One-line test repair (only test change):**
  `TestScarScrub_LaunchReturnsImmediatelyAndMarks` overrides
  `f.hooks.verify` with a marker-check hook that never appends to
  `f.verified`, then asserts `len(f.verified) > 0` — unobservable as
  authored (RED defect). Added the recorder append inside the override;
  assertion intent (verification ran with marker set) preserved.

## Verification (all VERIFIED, run in this worktree)

- `go test ./internal/core/ -run 'ScarScrub' -count=1 -v` → 6/6 PASS, exit 0.
- `go test ./internal/core/ -count=1` → ok (88s), 0 failures.
- `go test ./... -count=1` → all ok (exit 0).
- `go build ./...` → PASS. gofmt clean (a pre-existing unformatted
  `integration_gate_test.go` from a concurrent strand was left untouched).
- Coverage against the ScarScrub suite: `run` 77.3%, `scarMarkerGuarded`
  88.9%, `recoverScrubInProgress` 84.0%, `restampQuarantine` 83.3%,
  `quarantineNow` 75.0%, `scrubVerdict` 75.0%, `launchScarScrub` 68.8%.
  0%-covered names are the unwired-in-tests production bodies
  (`wireScarScrubHooks`, `scarQuarantineChain`, `scarFallbackForeign`,
  `scarLegacyQuarantine`, `MarkScrubInProgress`, `beadIDStrings`) —
  exercised live, not contract-covered.

## NOT DONE / NOT VERIFIED

- `QUARANTINE_SCAR_ENFORCE=1` gate run: suite passes with or without the
  gate (flag now moot — `scarScrubImplemented=true`); not separately run
  under the env var. A no-op either way.
- Live-dogfood observation of a restart mid-scrub (AC5) — unit-tested only.
- Concurrent-scrub probe-context cross-read never observed live.

## For the root/next bead

- One-scrub-per-key is enforced only in-memory (`scarScrubMarkers`); a
  restart-during-scrub + duplicate completion could double-run the decision.
  Registry Status mitigates; acceptable for GREEN.
- `docs/PRDs/quarantine-truth-prd-20261108.md` FR-002
  (`quarantine_unverifiable.go`, bead obvaq) was landing concurrently in this
  worktree and transiently broke compilation mid-edit at 17:03; resolved by
  its owning strand within the session (`go build ./...` PASS after 90s).
  Ran `go test ./...` AFTER that resolved — green across the board.
