# PRD: Quarantine Truth — Completion-Time Scar Scrub + Unverifiable-Scar Exit

**Version:** 1.2 · **Date:** 2026-11-08 · **System Type:** library · **Repo:** components/hydra

_Amended 2026-11-08 (round 1, REWORK → 7 amendments: gated-reconciler,
clean-worktree, no-command vs fault split, async+budget, parseable-output,
chain-union attribution, persisted counter, who-skip). Amended 2026-11-08
(round 2, SHIP-WITH-AMENDMENTS → findings 1-8 incorporated below): gate on
the post-merge scratch tree, goroutine owns the decision end-to-end +
restart recovery, panic-safe marker with wall-clock cap, move-not-duplicate
gate, junk-safe commit, per-tick streak sweep, singleton path named._

## Problem

Hydra quarantined the entire kzlu chain (31 beads, five independent feature
chains under one APPROVE root) on 2026-11-08 because ONE member — `m5ti`
(GREEN: Deterministic payload drift-check) — carried a stale `closed-failed`
label. The implementation `m5ti` was closed-failed for had landed in the main
checkout via another path (`internal/core/spawn_preflight.go` exists and the
project suite is green), but the scar was never scrubbed, and the
chain-completion callback's `ChainHasFailedBeads` check quarantined the whole
branch. Recovery required a hand revalidation of the quarantine branch and a
manual merge (commit `7a7c04b6`).

The existing label-hygiene machinery almost covers this and is the foundation
to build on:

- **M3 close-scrub** (`bead_closer.go:511`): a close with reason `completed`
  removes `closed-failed`. Never fired — `m5ti` never re-closed.
- **M1 quarantine revalidation** (`quarantine_reconciler.go:360`
  `revalidateMemberLocked`): every 5-minute tick, a tainted member's
  verification command is rerun in a scratch checkout of the quarantine
  branch; green scrubs the label. Fired for `m5ti` and looped forever:
  `revalidation of hydra-m5ti: ran=false passed=false — closed-failed stands`.
  `ran=false` means `verificationCommandFor(bead)` returned no command
  (`bead_closer.go:222-224`) OR the gate runner faulted — two very different
  conditions today collapsed into one forever-loop.

Two defects remain:

1. **The scar is decided at the wrong time with the wrong evidence.** The
   quarantine decision runs `ChainHasFailedBeads` AFTER all members closed —
   but the chain worktree (the member's own code at its own branch tip) still
   exists at that exact moment and is discarded by
   `QuarantineBranchAndRemove` without ever being consulted. The M1 path then
   reconstructs evidence in a scratch checkout with a cooldown — slower,
   weaker, and only AFTER the whole-branch quarantine is done.
2. **`ran=false` has no exit.** A member whose verification cannot run (no
   command declared) loops `ran=false → label stands` on every tick, forever,
   with no operator-visible signal.

## Goals

- A chain quarantines only for members whose failure is REAL at completion
  time — stale scars whose behavior is green in the chain worktree are
  scrubbed BEFORE the quarantine decision.
- Members that cannot be verified get a bounded, visible exit
  (`closed-failed-unverifiable` label + typed event), not a silent
  forever-loop — and infrastructure faults never count toward it.
- No new wedge and no ungated merge: every added check fail-opens on
  infrastructure faults; every integrate path (including the reconciler's)
  runs the suite gate.

### FR-001: Completion-time scar scrub in the chain worktree

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/chain_completion_initializer.go`, `internal/core/bead_closer.go`, `internal/core/quarantine_reconciler.go`, `internal/core/worktree_manager.go`, `internal/core/chain_completion_detector.go`, `internal/core/quarantine_scar_test.go`
**Test Command:** `go test ./internal/core/ -run 'ScarScrub' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none

(a) **Async scrub that owns the decision end-to-end.** The detection loop is
serial (`checkChainCompletion` invokes callbacks inline) and deletes the
chain entry immediately after the callback returns
(`chain_completion_detector.go` ~155-168) — so a synchronous scrub would
head-of-line-block every other chain for hours, and a "re-decide next tick"
design is impossible (nothing revisits the key). Instead: when
`ChainHasFailedBeads` reports tainted members, the callback launches the
scrub goroutine and RETURNS; the goroutine alone performs scrub → re-decide
→ quarantine-or-integrate (below). Marker discipline: a guarded in-memory
map keyed by chain key, set before the goroutine starts and cleared via
`defer` inside it, with `recover()` in the same defer (a panicked scrub must
clear its marker and fail-open to quarantine, not hold the key forever).
Wall-clock cap on the whole scrub: **3 hours** (N × 90-min verification
serial worst case is bounded by the cap; on expiry the goroutine takes the
fail-open branch). Restart recovery: in-flight scrub keys are persisted as
registry entries with status `scrub-in-progress` (written by the callback
before launching, same file the reconciler reads); on daemon start the
reconciler bootstrap finds them, and if the chain branch still exists
(`hydra/wt-<key>`) it re-runs the scrub decision from scratch; if the branch
is gone the entry is dropped. Total scrub budget per chain: the
verification-command rerun OR the test-gate fallback per member, plus ONE
shared full-suite run when ≥1 member uses the fallback — members share the
worktree, so one gate artifact serves all fallback members (the shared suite
runs via `RunChainSuiteGate`, the same entry point the choke point uses).
On budget exhaustion or timeout: fail-open to quarantine (M1 reconciler
remains the net).

(b) **Clean-worktree requirement.** Failed closes never commit
(`bead_closer.go:425` commits only on `reason == "completed"`); a tainted
member's work may sit uncommitted in the shared worktree. Green-on-uncommitted
is false green (the integrate merges the committed tip — uncommitted impl
would be discarded while its scrub said green). Before any scrub verdict, the
worktree must be clean: `git status --porcelain` empty. If dirty, the daemon
commits the pending changes first via the existing daemon-commit path — but
junk-safe: the commit excludes the known junk signatures (the clean-hands
`internal/junksig` structural-signature matcher, already wired into the
close-time junk gate), and any path the matcher flags is left uncommitted
(and reported in the scrub log), so a green scrub can never bake worker junk
into the chain branch. If the commit itself fails: skip the scrub for this
chain (fail-open to quarantine).

(c) **Verification rerun + parseable fallback.** Per tainted member, in the
chain worktree: if `verificationCommandFor(bead)` yields a command, rerun it
(`RunVerificationGateForRevalidation`, `bead_closer.go:221`) — green scrubs.
If NO command is declared, use the project test gate **shared run** from (a):
a scrub requires (1) the gate exited non-zero, (2) `parseFailingGoPackages`
extracted ≥1 failing package, and (3) NO failing package intersects the
**chain-union changed files** (`changedFilePaths(worktreePath)` — the
merge-base diff covering all members' committed work; NOT per-bead targets,
which can be absent or stale — a failure in any package the chain touched
must block the scrub). Zero parsed packages with non-zero exit (unparseable
output, non-Go projects without FAIL-line shape) = NO scrub (fail-open to
quarantine; the parse is Go-shaped and this PRD does not extend it). A
failing package intersecting chain changes = verified red = scar stands.

(d) **Scrub → re-decide → gated integrate, with re-stamp on gate failure.**
After scrubbing scrubbable members, re-evaluate `ChainHasFailedBeads`
(beadFileReader reads are uncached — fresh labels): if taint remains,
quarantine as before. If no taint remains, take the NORMAL integrate path
(`IntegrateBranchAndRemove` — the suite-gate choke point applies). If that
choke gate FAILS post-scrub: re-stamp `closed-failed` on the previously
tainted members (plus `closed-failed-suite`) BEFORE `Record()` so the
quarantine registry reflects reality and the reconciler's
`chainHasFailedBeadIDs` still sees taint — a scrubbed-then-gate-failed chain
must not become an ungated merge (review finding 1: the reconciler's
`IntegrateBranch` path is not the gated `IntegrateBranchAndRemove`).
Emit `scar_scrub_failed` (reason `suite_gate_post_scrub`) for triage parity.

(e) Scrub success = remove `closed-failed` + add
`closed-failed-revalidated` (existing label vocabulary) + emit
`label_revalidated` with reason `completion_scrub`. Scrub failure for a
member = label stands silently EXCEPT the (d) re-stamp path and a
`scar_scrub_failed` event when a member ends the scrub still tainted AND
unverifiable-by-this-pass (no command, no parseable fallback) — that member
is the FR-002 counter's head start.

**Acceptance Criteria:**
- A chain whose only tainted member is stale (verification command green in the chain worktree, or test-gate fallback green with ≥1 parsed failing package all foreign to chain-union changes) integrates through the gated path — no quarantine, `label_revalidated`/`completion_scrub` event emitted.
- A dirty worktree is committed junk-safe (junksig-flagged paths excluded) before any verdict; commit failure skips the scrub (quarantine).
- A panicked or timed-out scrub (3h cap) clears its marker via defer+recover and fails open to quarantine.
- A post-scrub choke-gate failure re-stamps `closed-failed` + `closed-failed-suite` before `Record()` and emits `scar_scrub_failed`.
- A daemon restart mid-scrub recovers: persisted `scrub-in-progress` entry re-runs the decision if the branch exists, drops it otherwise.


### FR-002: Unverifiable-scar exit — bounded attempts, operator-visible verdict

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/core/quarantine_reconciler.go`, `internal/core/bead_closer.go`, `internal/core/quarantine_unverifiable_test.go`
**Test Command:** `go test ./internal/core/ -run 'UnverifiableScar' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-001

(a) **No-command ≠ runner fault.** `RunVerificationGateForRevalidation`
(`bead_closer.go:221`) today returns `(false, false)` both for
no-command-declared and runner-fault — FR-002 splits these: it returns a
three-way result (or the caller checks `verificationCommandFor` first): only
**no-command-declared** increments the unverifiable streak; a **runner
fault** (command exists, execution faulted/timeout) does NOT count —
infrastructure failures fail open and keep retrying on the existing cooldown.

(b) **Concrete persisted counter.** `QuarantinedChainEntry` gains
`RanFalseStreaks map[string]int` (member ID → consecutive no-command
outcomes), persisted via the existing registry save path. Reset semantics:
increment on each no-command revalidation attempt; reset to 0 for a member on
(a) verified-green scrub, (b) verified-red (`ran=true`), or (c) operator
label removal — NEVER on the periodic `loadLocked` (it only adds entries
missing from memory; a per-tick reset would make the streak unreachable). On
daemon restart the persisted map restores progress. A per-tick sweep in `reconcileEntryLocked` resets a
member's streak to 0 when the member no longer carries `closed-failed`
(covers operator label removal — the reset fires without needing the member
to be re-selected). After **3 consecutive**
no-command outcomes: stamp `closed-failed-unverifiable` (alongside
`closed-failed`), emit `quarantine_unverifiable` (member ID + attempt
count), and skip the member in future `who` selection.

(c) **`who` selection skips stamped members.** `chainHasFailedBeadIDs`
returns the FIRST `closed-failed` carrier as the revalidation target; the
selection must skip members carrying `closed-failed-unverifiable` so the next
unstamped tainted member gets its turn. The per-entry cooldown
(`e.LastRevalidation`, 30 min) serializes members — N tainted members take
N × 30 min before all are stamped; this is documented behavior, not a defect
(the stamped member stops consuming ticks).

(d) **Quarantine holds (fail-closed).** `chainHasFailedBeadIDs` treats
`closed-failed-unverifiable` members as tainted — the label exists to make
triage one `br list --label closed-failed-unverifiable` away, not to
auto-integrate unverifiable work. The operator escape hatch is unchanged:
hand-verify (as in the kzlu recovery), remove both labels, and the next
reconciler tick integrates through the reconciler path — which FR-003 gates.

**Acceptance Criteria:**
- Runner faults (command exists, execution faulted) never increment the streak; only no-command-declared increments.
- 3 consecutive no-command outcomes stamp `closed-failed-unverifiable` + emit `quarantine_unverifiable`; the member is skipped in `who` selection.
- `RanFalseStreaks` survives daemon restart via the registry file; the per-tick sweep resets streaks for members no longer carrying `closed-failed`.
- Quarantine holds for unverifiable members (fail-closed); operator label removal resets the streak and re-enables re-verdict.


### FR-003: Reconciler integrate runs the suite gate

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/quarantine_reconciler.go`, `internal/core/worktree_manager.go`, `internal/core/quarantine_gate_test.go`
**Test Command:** `go test ./internal/core/ -run 'QuarantineGate|ReconcilerIntegrateGated' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none

The reconciler's re-verdict path calls `IntegrateBranch`
(`quarantine_reconciler.go:296`) — NOT the gated
`IntegrateBranchAndRemove`; the 2026-11-08 integration-gate work gated only
the latter. Every path that merges a quarantine/chain branch into the
working branch must run the suite gate: **move** (not duplicate — a
duplicate would double the full suite on every clean chain completion) the
choke-point gate from `IntegrateBranchAndRemove` into `IntegrateBranch`
(the common inner merge both variants share), preserving the nil-gate
fail-open and the `chain suite gate:` error prefix.

**Gate placement must survive quarantine teardown:** the existing gate block
runs only `if isGitWorktree(m.worktreePath(projectPath, key))` — but the
reconciler's path arrives AFTER `QuarantineBranchAndRemove` removed that
worktree, so a naive copy no-ops exactly here. The gate therefore runs on
the post-merge verification tree `IntegrateBranch` already materializes (the
`integrate-scratch-<key>` worktree), which exists on every path — chain
worktree present or not. Error contract: `IntegrateBranch` returns the gate
failure as a distinct typed error (or sentinel-wrapped), and the reconciler
matches on it — no string sniffing bolted on at the call site.

This closes the FR-001(d) re-stamp gap at the root (a scrubbed chain whose
branch is red can never merge ungated, whatever path fires) and the
FR-002(c) operator-escape merge. Known additional path newly gated: the
singleton close fallback (`bead_closer.go` ~475-489, `Integrate` →
`IntegrateBranch`) — a deliberate behavior change per the Goals ("every
integrate path"); the PR-first singleton path (`publishSingletonViaPR`)
bypasses `IntegrateBranch` and remains ungated (out of scope, documented
here). On gate failure the reconciler keeps the quarantine (fail-closed),
emits `quarantine_suite_gate_blocked`, and continues re-verdicting.


**Acceptance Criteria:**
- The suite gate runs on the `integrate-scratch-<key>` post-merge tree inside `IntegrateBranch` (moved from `IntegrateBranchAndRemove`, not duplicated) — the reconciler's branch-only path and the singleton close fallback are both gated.
- Gate failure on the reconciler path keeps the quarantine, emits `quarantine_suite_gate_blocked` (typed error matched, no string sniffing).
- Nil gate fails open; a clean chain completes with exactly ONE full-suite run across its integrate.

## Non-Goals

- Splitting a quarantined branch into per-sub-chain branches (branch surgery;
  the scrub makes it unnecessary in the observed class).
- Auto-integrating unverifiable members (fail-closed stays).
- Extending `parseFailingGoPackages` beyond Go-shaped `FAIL\t<pkg>` output
  (non-Go projects simply never scrub via the fallback — quarantine + M1
  command path + operator triage remain).
- Touching the per-bead close gates or the RED-phase gates (landed
  2026-11-08, commits `7c8ed619`/`cefc1b7a`).

## Evidence Appendix

- Incident: quarantine log `2026/11/08 14:23:29 [Worktree] quarantined failed
  chain hydra-kzlu`, 31 beads, five feature chains (Turn distribution,
  Brownfield enrichment, Turn telemetry, Claim re-proof, Payload drift-check).
- Stale scar: `m5ti` closed-failed; impl present at
  `internal/core/spawn_preflight.go`; suite green on quarantine branch
  (20 ok / 0 FAIL, hand-verified pre-merge `7a7c04b6`).
- Forever-loop: `2026/11/08 14:28:29 [QuarantineReconciler] hydra-kzlu:
  revalidation of hydra-m5ti: ran=false passed=false — closed-failed stands`.
- Ungated reconciler merge path (review finding 1):
  `quarantine_reconciler.go:296` `IntegrateBranch` vs gated
  `IntegrateBranchAndRemove` (`worktree_manager.go` choke point).
- Uncommitted-work blindness (review finding 2): `bead_closer.go:425`
  commits only on `reason == "completed"`.
- ran=false conflation (review finding 3): `bead_closer.go:221-252` returns
  `(false, false)` for both no-command and runner fault.
- Serial callback loop (review finding 4): `chain_completion_detector.go:148-162`
  invokes callbacks inline; verification timeout 90 min (`bead_closer.go`).
- Vacuous-foreign false green (review finding 5): `gate_close.go`
  `isForeignTestFailure` requires ≥1 parsed package; the PRD fallback now
  requires the same.
- Round-2 evidence: detector deletes chain info post-callback
  (`chain_completion_detector.go` ~155-168; reconciler header ~25-26
  confirms); quarantine teardown removes worktree+branch before reconciler
  integrate (`worktree_manager.go` ~1073-1090) — gate-on-scratch-tree
  requirement; `commitLocked` excludes only `.beads` (~737-741) — junk-safe
  commit requirement; singleton fallback `Integrate` → `IntegrateBranch`
  (`bead_closer.go` ~475-489).
- Existing machinery to reuse: `RunVerificationGateForRevalidation`
  (`bead_closer.go:221`), `parseFailingGoPackages`/`goPackageToDir`/
  `changedFilePaths` (`gate_close.go`), label vocabulary
  `closed-failed-revalidated` (`quarantine_reconciler.go`), registry
  persistence (`quarantined-chains.json`, `saveLocked`/`loadLocked`).
