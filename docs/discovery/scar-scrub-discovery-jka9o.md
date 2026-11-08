# Discovery — FR-001 Completion-time scar scrub in the chain worktree

PRD: `docs/PRDs/quarantine-truth-prd-20261108.md` (v1.2). Bead: hydra-jka9o (discovery).
Incident: the 2026-11-08 kzlu chain quarantine — one stale `closed-failed`
scar (`m5ti`) quarantined 31 beads across five feature chains, recovered by
hand revalidation + manual merge `7a7c04b6`.

**No production code changed by this bead** (discovery phase of the TDD chain).
This doc resolves the ambiguities RED/GREEN need; every claim below was
verified against this worktree at bead start.

## 1. Verified state of the machinery (code inspection)

| Site | File:line (approx) | Verified fact |
|---|---|---|
| Quarantine decision | `chain_completion_initializer.go:155` | `ChainHasFailedBeads(event.ChainInfo.AllBeadIDs)` gates `QuarantineBranchAndRemove` → `quarantineReconciler.Record(...)` → `chain_quarantined` event. Runs inline in the callback; detector then deletes chain info (`chain_completion_detector.go:148-168`). |
| Taint oracle | `chain_completion_detector.go:190` `ChainHasFailedBeads`; `quarantine_reconciler.go` `chainHasFailedBeadIDs` | Both read `closedFailedLabel` from `BeadFileReader.FindByID` — **uncached**, so a fresh label read inside a scrub goroutine sees the scrub's own writes. |
| Close-time scrub (per-bead, already landed) | `bead_closer.go:499-526` | A `reason == "completed"` close removes `closed-failed` + stamps `closed-failed-revalidated` + emits `label_revalidated` (`reason completed_close_scrubs_closed_failed`). FR-001 reuses the same label/event vocabulary with reason `completion_scrub`. |
| Revalidation that loops today | `bead_closer.go:221` `RunVerificationGateForRevalidation` → `(passed, ran bool)` | `ran=false` conflates no-command, nil gate runner, and runner fault (`bead_closer.go:227, 249`). FR-002 splits these; FR-001 GREEN needs at minimum a distinguishable "no command declared" so the fallback path is reachable. |
| Gated integrate choke point | `worktree_manager.go:897` `IntegrateBranchAndRemove` | Suite gate runs `if isGitWorktree(m.worktreePath(projectPath, key))` (worktree-gated!), then `IntegrateBranch` merges in the `integrate-scratch-<key>` scratch tree. Gate failure surfaces as error string `chain suite gate: ...` — sniffer at `chain_completion_initializer.go:192`. **FR-003 is a sibling bead that moves this gate into `IntegrateBranch` on the scratch tree as a typed error.** |
| Quarantine registry | `quarantine_reconciler.go:69-101` `QuarantinedChainEntry{ProjectPath, RepoName, Key, Branch, MemberIDs, QuarantinedAt, IntegrateAttempts, LastRevalidation}` + `saveLocked/loadLocked` over `$HYDRA_HOME/quarantined-chains.json` | No `Status`, no `RanFalseStreaks` today. `Record` is fail-open and written by the callback AFTER `QuarantineBranchAndRemove` succeeds. |
| Worktree existence at decision time | `worktree_manager.go:315/275` | The chain worktree (`worktreePath(projectPath, key)`) **still exists** when `ChainHasFailedBeads` runs; only `QuarantineBranchAndRemove` (`worktree_manager.go:1082`+) removes worktree+branch. This is the window the scrub uses. |
| Shared suite gate entry point | `gate_close.go:231` `RunChainSuiteGate(projectName, projectPath, worktreePath, chainKey)`; wired as `SuiteGate` at `overseer.go:753` | Same callable the choke point uses — the scrub's shared run goes through `m.SuiteGate`/`defaultWorktreeSuiteGate`, satisfying "one gate artifact serves all fallback members". |
| Foreign-failure attribution | `gate_close.go:317` `parseFailingGoPackages` (Go-shaped `FAIL\t<pkg>` lines only), `goPackageToDir`, `changedFilePaths(worktreePath)` (merge-base `origin/development`→HEAD diff, `gate_close.go:355`) | The PRD chain-union attribution is `parseFailingGoPackages(gateOutput)` ∩ `changedFilePaths(worktreePath)`. Non-Go output yields 0 parsed packages → no scrub (documented non-goal). |
| Dirty-work commit path | `worktree_manager.go:728-747` `commitLocked` | Commits everything except `.beads` (`add -A -- . :(exclude).beads`), refuses conflict markers, guards embedded repos. **No junk filtering.** `internal/junksig.IsJunkPath` (already green, 10/10 tests) is the matcher used by the close-time junk gate (`gate_junk.go`). |
| Failed closes never commit | `bead_closer.go:425` | Only `reason == "completed"` commits — green-on-uncommitted is real: a tainted member's impl may sit as untracked/modified files in the shared worktree. |

Confirmed **absent** (rg, zero hits across `internal/`): `closed-failed-suite`
label, `scar_scrub_failed` event, `completion_scrub` reason, `scrub-in-progress`
registry status, any scrub marker map, any wall-clock scrub cap.

## 2. Ambiguities resolved for GREEN (owners pinned)

**A1 — Async goroutine owns the decision end-to-end; snapshot before return.**
The detector deletes chain info for the key right after the callback returns,
so the goroutine must capture everything it needs from `event.ChainInfo`
(projectPath, key/worktreeKey, recorded branch, AllBeadIDs, completedAt)
plus the overseer's live handles (`worktreeManager`, `beadCloser.events`,
`beadsClient`, `quarantineReconciler`) before the callback returns. The
`extractor.ExtractPatterns(event)` step and the pre-decision
`RunPRDValidation` stay synchronous in the callback (PRD validation does not
gate the scrub; it never reads labels). `RunPRDValidation` for the
scrub-yields-integrate path re-runs inside the goroutine only if it was not
run in the callback (single execution, logged, fail-open on error, exactly as
today).

**A2 — Where to scrub: live chain worktree first, scratch fallback second.**
Primary evidence surface is `m.worktreePath(projectPath, key)` when
`isGitWorktree(...)`. If the worktree is gone but the resolved branch exists
(versioned `hydra/wt-<key>-vN` case: `resolveChainBranch` fallbacks), scrub in
a detached scratch checkout of the branch tip (reconciler pattern:
`worktree add --detach -c core.hooksPath=/dev/null`, node_modules symlink,
force-remove + prune in defer — `quarantine_reconciler.go:380-395`). If
neither exists: the chain has nothing to preserve — do NOT run
`QuarantineBranchAndRemove` (it returns ""), keep the existing
`chain_integration_unverified / no_worktree_or_branch_after_resolution` skip,
and skip the scrub silently (no marker churn).

**A3 — Marker: in-memory map + persisted `scrub-in-progress` entries.**
New guarded map on the overseer (or detector) keyed by chain key, set by the
callback before `go scrub(...)`. `defer func(){ if recover() != nil {
log + failOpenToQuarantine() }; clearMarker() }()` inside the goroutine.
Wall-clock: `context.WithTimeout(3 * time.Hour)` around the whole scrub; on
expiry the goroutine takes the fail-open branch (quarantine if
`QuarantineBranchAndRemove` still has work to preserve, else clear marker).
Persistence: `QuarantinedChainEntry` gains `Status string` (+ `RanFalseStreaks`
lands with FR-002); the callback writes an entry `{Key, ProjectPath, RepoName,
MemberIDs, Status: "scrub-in-progress", QuarantinedAt: now}` via a new
`quarantineReconciler.RecordScrubInProgress(...)` BEFORE launching, and the
goroutine deletes it (or rewrites it to a real quarantine entry) via
`deleteScrubInProgress(key)` in its defer. Restart recovery: at reconciler
bootstrap (`loadLocked` post-process), entries with
`Status=="scrub-in-progress"` are resolved — if a chain branch
(`hydra/wt-<key>` exact, else `newestVersionedBranch`) exists in
`e.ProjectPath`, re-run the full decision FROM SCRATCH (taint re-read →
scrub → integrate/quarantine); if no branch exists, drop the entry. The
recovered decision must reuse the same goroutine entry point the callback
uses (single implementation, so recovery cannot drift from the live path).

**A4 — Junk-safe dirty-worktree commit.**
Before any scrub verdict, `git status --porcelain` in the scrub tree: if
non-empty, run a dedicated `junkSafeCommit(scrubDir)` — a new helper that
(1) lists untracked+modified paths, (2) partitions them by
`junksig.IsJunkPath`, (3) `git add -A -- . ':(exclude).beads' <one (exclude)<junkpath> per junk path>`,
reusing `commitLocked`'s embedded-repo guard + conflict-marker refusal
semantics (extraction: `commitLocked` grows an optional exclude-list param or
the helper does its own add/commit pair — GREEN's choice, behavior pinned by
tests), (4) leaves junk paths uncommitted on disk and logs them in the scrub
log. Commit failure (any error incl. conflict markers) → skip the scrub,
fail-open to quarantine. Junk-only dirt (`porcelain` non-empty but every path
junk) → no commit needed; proceed (junk is not work).

**A5 — Fallback verdict shape (the AC's "test-gate fallback green").**
One shared suite run per chain (`gate(projectPath, scrubDir, key)` via
`m.SuiteGate`) serving every no-command member. Fallback verdict green when:
gate **exit 0** (suite green in the worktree — strongest positive evidence),
OR gate non-zero AND `parseFailingGoPackages` ≥1 AND every failing package
foreign to `changedFilePaths(scrubDir)` (chain-union). Gate non-zero with 0
parsed packages, or ≥1 failing package intersecting chain changes → fallback
red, scar stands. Parseable-output sourcing: `RunChainSuiteGate` must surface
the gate artifact's output alongside its error for the parse (today it
returns only `error`; GREEN adds an output-returning variant or reads the
gate artifact's raw log via the existing artifact path — implementation
detail pinned by RED tests).

**A6 — Per-member verification rerun.**
For each tainted member with `verificationCommandFor(bead) == ok`: run
`RunVerificationGateForRevalidation(bead, projectPath, scrubDir)` ONLY when
the scrub dir resolves (A2); the 90-minute runner timeout stands (3h cap
bounds ≥2 serial reruns; budget: rerun-OR-fallback per member + ONE shared
suite run). `ran=false` here is ambiguous pre-FR-002; FR-001 GREEN needs the
split minimally: check `verificationCommandFor` explicitly first (no-command
→ fallback path), treat runner faults as fail-open (label stands, streak NOT
counted — FR-002 contract, noted so GREEN doesn't conflate again).

**A7 — Scrub → re-decide → gated integrate with re-stamp.**
After all scrubbable members are processed, re-run taint over
`e.MemberIDs` via `chainHasFailedBeadIDs` (uncached reader). Taint remains →
quarantine exactly as the callback would have (`QuarantineBranchAndRemove` +
`Record` + `chain_quarantined`). Clean → rewrite the scrub-in-progress entry
to a normal integrate decision: call the gated path. Gate-failure detection:
match FR-003's typed error if present, else the existing `"chain suite gate"`
prefix — GREEN ships a single `isChainSuiteGateError(err)` helper used by
both call sites so the string sniffer at `chain_completion_initializer.go:192`
collapses into it. On gate failure: for every member just scrubbed,
`UpdateBeadLabels` re-stamp `closed-failed` + `closed-failed-suite` BEFORE
`quarantineReconciler.Record(...)`, then `QuarantineBranchAndRemove` +
`Record` as today, and emit `scar_scrub_failed` (reason
`suite_gate_post_scrub`, chain key + member list + gate error in Fields).
The re-stamp preserves the reconciler's `chainHasFailedBeadIDs` taint so the
scrubbed-then-red chain re-verdicts with M1 evidence instead of merging
ungated.

**A8 — Scrub success bookkeeping.**
Per scrubbed member: remove `closed-failed`, add
`closed-failed-revalidated`, emit `label_revalidated` (reason
`completion_scrub`) — mirror of `bead_closer.go:511-526`. A member that ends
the scrub still tainted AND unverifiable-by-this-pass (no command, no
fallback evidence) emits `scar_scrub_failed` (reason
`unverifiable_by_this_pass`) — FR-002's counter head start; label stands.

**A9 — Interactions to not break.**
- M1 reconciler keeps running: a failed scrub leaves the label, quarantine +
  `Record` as today, M1 revalidates later — net unchanged.
- `ChainHasFailedBeads` consumers elsewhere (scheduler/Charon) are untouched.
- The scrub must NOT remove `closed-failed-suite` stamps or
  `closed-failed-unverifiable` stamps (FR-002 vocabulary) — they are
  deliberate evidence, not scars.

**A10 — Concurrency.**
One scrub per chain key (marker); a second completion callback for the same
key (should not exist post-deletion) is logged and dropped. Scrubs and the
reconciler never touch the same entry concurrently: the reconciler only sees
the key AFTER fail-open quarantine rewrites the entry.

## 3. Open questions for GREEN (declared, with default answers)

1. `RunChainSuiteGate` output plumbing for the parse (A5) — default: extend
   the gate seam to return `(output string, err error)`; RED tests pin it.
2. `commitLocked` exclusion param vs a dedicated helper (A4) — default:
   dedicated `junkSafeCommit`, `commitLocked` untouched.
3. `Status` field JSON shape for the registry (A3) — default: `"status":
   "scrub-in-progress"`, absent field for legacy entries (empty string),
   `loadLocked` tolerant of both.

## 4. Test surface sketch for RED (quarantine_scar_test.go)

Gated per the established codebase pattern (`ScarScrub`-selector tests, skip
loudly via `scarScrubRequireImpl`/`SCAR_SCRUB_ENFORCE`, ENFORCE run proves
RED): stale-member chain integrates with `label_revalidated{completion_scrub}`
and no `chain_quarantined`; dirty-worktree junk-safe commit excludes
junksig paths and a commit failure quarantines; panic + timeout clear the
marker via defer/recover and quarantine; post-scrub gate failure re-stamps
`closed-failed`+`closed-failed-suite` before `Record` + emits
`scar_scrub_failed`; persisted `scrub-in-progress` entry with live branch
re-runs the decision and with no branch is dropped.

## 5. Sequencing recommendation

FR-003 (move the gate into `IntegrateBranch`, typed error) has no dependency
and is the smaller, root-cause change; landing it first gives FR-001(d) its
typed error and shrinks A7 to a sentinel match. FR-001 GREEN must compile and
pass its tests against EITHER ordering (hence `isChainSuiteGateError`).
