# RED-phase handoff — Completion-time scar scrub (FR-001, quarantine-truth PRD)

Bead: `wmxqe` (RED, chain worktree strand of the quarantine-truth PRD `docs/PRDs/quarantine-truth-prd-20261108.md`).
Phase: RED — contract tests only. HARD RULE respected: no production behavior written.

## What landed

- `internal/core/quarantine_scar_test.go` — contract tests for all five parent ACs, `TestScarScrub_*` (6 top-level tests incl. subtests).
- `internal/core/quarantine_scar.go` — RED inert compile surface: constants (`scrubInProgressStatus`, `scarScrubFailedEvent`, `scarScrubFailedReason`, `scarScrubRevalidatedReason`, `closedFailedSuiteLabel`, `scarScrubWallClockCap=3h`), `scarDecision` enum, `scrubHooks` dependency seams, `chainScarScrubber`, `scarMarkerGuarded`, `scarScrubActive`, `launchScarScrub`, `scrubRegistryEntry` (registry entry + `Status`), `recoverScrubInProgress`, `scarJunkPath` / `scrubWorktreeBranch` seams. `scarScrubImplemented=false`; every non-seam body returns the fail-closed decision or does nothing.
- `CHANGELOG.md` — Unreleased entry (RED + gate name + ACs).

## RED discipline — VERIFIED

`go test ./internal/core/ -run 'ScarScrub' -count=1 -v` → 6/6 SKIP with the skip-loudly message ("scar scrub not implemented yet; set QUARANTINE_SCAR_ENFORCE=1 to fail loudly (RED discipline)"), PASS overall.

`QUARANTINE_SCAR_ENFORCE=1 go test ./internal/core/ -run 'ScarScrub' -count=1 -v` → all 6 FAIL at the enforcement gate with the exact message `QUARANTINE_SCAR_ENFORCE=1: completion-time scar scrub not implemented yet — RED at the enforcement gate`. `go vet` clean; package compiles.

Coverage: `go test ./internal/core/ -run 'ScarScrub' -coverprofile=/tmp/scar.cov` → `coverage: 0.0% of statements`. `go tool cover -func` on the new stub functions: `defaultScrubHooks`, `newChainScarScrubber`, `run`, `scarMarkerGuarded`, `scarScrubActive`, `launchScarScrub`, `recoverScrubInProgress` — all 0.0%. Expected at RED: the inert bodies are never exercised because `requireScarScrubImpl` gates every test. GREEN should target ≥80% on `quarantine_scar.go`'s scrub paths.

## Pinned contract (what GREEN must satisfy)

1. **AC1** — chain whose only taint is a stale `closed-failed` member: green member verification rerun → scrub `closedFailedLabel`, add `closedFailedRevalidatedLabel`, emit exactly one `label_revalidated` event with reason `completion_scrub`, integrate via the gated path, zero quarantine calls. Fallback subtest: member has no command (`ran=false`) → shared suite gate green + `fallbackForeign=(true,true)` → still integrated. Decision `scarIntegrated`.
2. **AC2** — dirty worktree: `scarJunkPath`-classified paths excluded from the `commitPaths` pre-verdict commit; commit failure → decision `scarQuarantined`, verification skipped entirely (no verify hook call), exactly one quarantine call.
3. **AC3** — verify-hook panic → `scarMarkerGuarded` recovers to `scarQuarantined` (test asserts the panic does NOT leak out) and `scarScrubActive` is false afterward; wall-clock cap (fixture uses `s.cap=50ms`; production default constant 3h asserted) → same outcome.
4. **AC4** — scrub green but `suiteGate` fails → `scarQuarantined`, integrate hook never called, labels added cover BOTH `closed-failed` and `closed-failed-suite` (one re-stamp, before the reconciler `Record()` of the quarantine), one `scar_scrub_failed` event with reason `suite_gate_post_scrub`.
5. **AC5** — registry entry carrying `Status: scrub-in-progress` after restart: branch exists (`branchExists` hook true for `hydra/wt-*`) → decision re-runs (verify hook runs); branch gone → entry removed from `r.entries`.
6. **Launch semantics** — `launchScarScrub` returns immediately (the detection loop deletes chain info inline) while the marker is visible during the scrub body and released after.

## Notable for GREEN

- The scar-scrub machinery is **async**: the completion callback (chain_completion_initializer.go ~112) must return immediately; `launchScarScrub` is the entry point. The detector calls callbacks inline and deletes chain info after return.
- `scrubRegistryEntry` is a stub-side extension of `QuarantinedChainEntry` with `Status`; fold a real `Status` field into `QuarantinedChainEntry` (quarantine_reconciler.go) at GREEN and make the registry persist it (`saveLocked`/`loadLocked` merge rules) and wire `recoverScrubInProgress` from `Reconcile()` bootstrap.
- Seams (`scrubHooks`) wire at production from: `RunVerificationGateForRevalidation` (bead_closer.go:221), `RunChainSuiteGate`/`parseFailingGoPackages`/`changedFilePaths` (gate_close.go), `junksig.IsJunkPath`, worktree manager integrate/quarantine, label ops via `beadsClient`.
- Env gate: `QUARANTINE_SCAR_ENFORCE` (no collision — checked; `scarScrubImplemented=false` must remain false until GREEN, else tests pass vacuously).

## Worktree hygiene note

This worktree had a concurrent sibling strand's uncommitted edits to `quarantine_reconciler.go` (transient duplicate `"errors"` import + `quarantine_unverifiable.go/_test.go` new files) mid-session, which briefly broke the package build (`errors redeclared in this block`, `undefined: unverifiableLabels`). I did NOT touch those files; the owning strand self-resolved within ~75s and my suite then ran clean. Their dirty files remain intentionally untouched.

## Test command

```
go test ./internal/core/ -run 'ScarScrub' -count=1 -v
QUARANTINE_SCAR_ENFORCE=1 go test ./internal/core/ -run 'ScarScrub' -count=1 -v
```
