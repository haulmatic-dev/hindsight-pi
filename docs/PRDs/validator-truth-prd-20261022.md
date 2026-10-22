# PRD: Validator Truth — Evidence-Based TDD Validation, Honest Strike Accounting, Self-Clearing Fix Beads

**Version:** 3.0 (two adversarial review rounds) · **Date:** 2026-10-22 · **System Type:** library · **Repo:** components/hydra

## Problem

TDD chain validation has never passed in the current log window: **0 PASS / 119 FAIL**
(109 hydra, 10 srv-data-harbour, 2026-10-15 → 2026-10-22). Every failure is the false
gap `RED bead <id> has no test files (target_files metadata missing)`. Root causes,
all verified against the code and the live store on 2026-10-22:

1. **Declarations, not evidence.** `TDDValidator.extractTargetFiles`
   (`internal/validation/tdd_validator.go:224`) reads only `Metadata["target_files"]`.
   Task-breakdown beads have empty metadata and put files in the description context
   JSON — and in ~60% of chain beads that list is empty anyway (store scan: RED 20/34,
   GREEN 20/34, REFACTOR 18/32, SYNTAX 20/37 have `"files": []`). Where it is present,
   RED beads list *implementation* files (hydra-367d → `internal/core/service_starter.go`),
   which `validateRedBead` would then treat as test files. Declarations cannot be the
   source of truth; the bead's own commits and uncommitted worktree changes can.
2. **Wrong chain discovery.** `findTDDChainBeads` (`internal/core/validation_service.go`
   ~335-367) inspects only direct dependencies and classifies with
   `strings.Contains(strings.ToLower(title), "red")` — 105 non-RED titles in the store
   contain "red" ("Sha**red** audit package", "clea**red**"). GREEN hydra-qfh9 depends
   only on RED hydra-367d, so the bead actually closing is never validated, and missing
   sides surface as gaps with an empty id (8 of 18 fix beads).
3. **Assertion heuristic is broken.** `hasMeaningfulAssertions` lowercases the content
   and then searches for `"t.Run"` and `"Test"` — never matchable.
4. **Rounds burn without new evidence.** `RunTDDValidation` increments the validation
   retry counter even when dedup created no fix bead, and verified-close
   (`bead_closer.go:1805`, read at `:1897`) increments and reads the **same**
   `validationRetries` map — one close attempt counts twice (hydra-qfh9: round 1
   16:25:09, round 2 16:25:51, escalated 16:26:13).
5. **`dep-open` costs a strike.** An upstream bead reopened by a rejection after its
   downstream started makes the downstream close fail `dep-open`
   (`bead_closer_hard_gates.go:202-203`), which counts toward escalation — 36 such
   rejections; 8 beads escalated on it alone.
6. **Fix beads cannot clear themselves.** FIX-AUDIT (`internal/core/fix_audit.go:40`)
   skips anything with `Type != "fix"`, and red-gap fix beads are created with type
   `test`, so 18 false `auto-fix` beads accumulated in one day and had to be closed by
   hand (2026-10-22, label `false-positive-tdd-target-files`).
7. **Dead guard in `clear-escalation`.** `service_starter.go:496` tests
   `!escLabels[escalatedLabel]` (the static map, always true) instead of the bead's labels.

Note: instant re-escalation after a Pan `clear-escalation` is the documented "one fair
retry" contract (`pan.go:1515`, `config.go:114`, shepherd.md) and is **not** changed here.

Interim operator hold: `validation.tdd_validation_enabled: false` in `/etc/hydra/hydra.yaml`.
FR-006 replaces the manual re-enable with an observable shadow→enforce rollout.

## Verified Facts (measured 2026-10-22)

- `br show hydra-367d --json`: `metadata` empty; description contains
  `"target": {"files": ["internal/core/service_starter.go"], ...}`.
  hydra-m1h9 and hydra-2bht carry `"files": []`.
- hydra-m1h9's real test work is agent commit 7809bef7
  (`test(beads-mirror-audit): RED-phase stub + contract tests … (hydra-m1h9)`, modifies
  `tools/beads-mirror-audit/main_test.go` +173/−47). It carries the bead id only in its
  subject — no `Bead:` trailer. The daemon's trailer commit 7a1f6491 (`Bead: hydra-m1h9`)
  touches only `CHANGELOG.md` and a discovery doc. Per-bead evidence must therefore match
  the bead id anywhere in the commit message, not just the trailer.
- `verifyTDDChain` runs before `resolveEvidenceRepo` and before the daemon's
  `CommitBead` (`bead_closer.go` ~1350/1354/424), so the closing bead's own work may be
  uncommitted when validation runs; chain branches integrate repeatedly
  (`hydra: integrate hydra/wt-hydra-8uw4` 7× in 60 merges), so a branch-diff window is
  not per-bead.
- 18/18 open `auto-fix` beads had the identical gap text; 8 had an empty RED id.
- Events 2026-10-21/22 for 18 hydra chain beads: 83 `tdd-validation-failed` and
  36 `dep-open` verified-close rejections.
- `TDDChainPasses` returns pass when validation is disabled (the hold is effective).

## Goals

- Validation verdicts are derived from what the chain committed.
- A strike is spent only once per attempt and only on new evidence.
- Hydra clears its own false fix beads.
- Re-enabling validation is a measured, logged step, not a guess.

### FR-001: Shared target-file resolver package (no adoption yet)

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/beadfiles/resolve.go`, `internal/beadfiles/resolve_test.go`
**Test Command:** `go test ./internal/beadfiles/ -run 'TestResolve_|TestIsTestFile_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none

Create `internal/beadfiles` with `Resolve(bead types.Bead) []string`. **First non-empty
source wins** (same semantics as `beadTargetFiles`): metadata `files`, then metadata
`target_files`, then the `target.files` array parsed **only** from the first fenced JSON code block (language tag `json`) following a `## Context` heading, then a `Files:` or `**Target Files:**`
line. Line parsing keeps `splitFileList` semantics (comma, `;`, space, tab separators;
drop `TBD`) plus backtick stripping. Strip a trailing `:symbol` suffix
(`a/b.go:Func` → `a/b.go`). Return repo-relative, de-duplicated, order-preserving paths;
malformed JSON is treated as absent (never panic). Also export
`IsTestFile(path string) bool` (`_test.go`, `.spec.ts`, `.test.ts`, `.spec.js`, `.test.js`,
`test_*.py`, `*_test.py`). `beadfiles` imports only the standard library and
`pkg/types` (no `internal/core` or `internal/validation`, keeping it cycle-free). Test
functions are named `TestResolve_*` and `TestIsTestFile_*`.
Update `CHANGELOG.md` (Added).

**Acceptance Criteria:**
- `TestResolve_*` fixtures: metadata-only; description-JSON-only using the hydra-367d
  description shape including trailing gate-log text after the JSON block;
  `"files": []` → empty slice; `**Target Files:**` backticked line;
  `Files: a.go; b.go TBD` → `[a.go b.go]`; `a/b.go:Func` stripped; malformed JSON →
  falls through to the next source.
- `TestIsTestFile_*` table covers every listed pattern plus one negative per language.
- The Test Command output lists ≥8 `--- PASS` lines and no `[no tests to run]`.
- `go build ./...` exits 0.

### FR-002: Correct TDD chain discovery

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/validation_service.go`, `internal/core/validation_chain_test.go`
**Test Command:** `go test ./internal/core/ -run 'TestTDDChain_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none

Add `findTDDChain(bead types.Bead, projectPath string) (ChainSides, error)` where
`ChainSides{Red, Green types.Bead; RedLocated, GreenLocated bool}`. It (a) walks
dependencies **transitively** (depth ≤ 8, visited set), (b) classifies RED/GREEN only by
label (`tdd-red`, `tdd-green`) or exact title prefix (`RED:`, `GREEN:`) — never a
substring, never `Type` alone, (c) chooses sides by the closing bead's phase: a RED bead
validates only itself as RED (GREEN not located); a GREEN bead validates itself as GREEN
plus its nearest RED ancestor; REFACTOR/SYNTAX beads use their nearest GREEN and RED
ancestors. Keep `findTDDChainBeads` with its current signature as a thin wrapper over
`findTDDChain` (returning zero values for unlocated sides and an error only when neither
side is located) so `overseer_chain_validation.go` keeps compiling. Test functions are
named `TestTDDChain_*`.

**Acceptance Criteria:**
- Live-shape fixture: GREEN depends only on RED; REFACTOR depends only on GREEN →
  closing REFACTOR locates both; closing GREEN locates itself as GREEN and the RED
  ancestor; closing RED locates only itself.
- `GREEN: Implement Shared audit package` classifies as GREEN, not RED;
  `REFACTOR: … with cleared premise` classifies as neither.
- A dependency cycle terminates; depth > 8 stops without panic.
- The Test Command output lists ≥5 `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-003: Per-bead evidence and a side-effect-free verdict

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/validation/bead_evidence.go`, `internal/validation/bead_evidence_test.go`, `internal/validation/tdd_validator.go`, `internal/validation/tdd_validator_test.go`, `internal/core/validation_service.go`, `internal/core/bead_closer.go`, `internal/core/overseer_chain_validation_test.go`, `internal/core/validation_dedup_test.go`
**Test Command:** `go test ./internal/validation/ -run 'TestBeadEvidence_|TestTDDValidator_' -count=1 -v && go test ./internal/core/ -run 'TestTDDVerdict_|TestRunTDDValidation' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-001, FR-002

(a) Add `BeadEvidence(repoRoot, beadID string, includeDirty bool) (Evidence, error)`
returning the files touched by every commit whose message contains `beadID` as a fixed
id (`git log --all -E --grep '<regex-escaped beadID>([^[:alnum:]]|$)' --name-only`, so
`hydra-5e8` never matches `hydra-5e85`), plus — when `includeDirty` — the dirty set of
`repoRoot` (`git status --porcelain=v1 -z --untracked-files=all`, rename `R old -> new`
form handled, excluding `.beads/`), and a fingerprint (sha256 over sorted `path:blob-id`). A missing git binary,
a non-repo directory, or a git error yields empty evidence, never an error that blocks.
(b) Add `ValidationService.TDDChainVerdict(bead types.Bead, projectPath, evidenceRoot string, includeDirty bool) (Verdict, error)`
with `Verdict{Located, Passed bool; Gaps []validation.TDDGap; Fingerprint string}`. It
emits no events, creates no beads and touches no counters. Rules: RED passes when its
evidence contains ≥1 `beadfiles.IsTestFile` file with a meaningful assertion; GREEN
passes when its evidence contains ≥1 non-test source file (not `IsTestFile`, not `*.md`,
not under `docs/`, not `CHANGELOG*`) that is not a stub. `includeDirty` applies only to
the closing bead, and only when the caller passes true. Declared files (`beadfiles.Resolve`) only scope the stub check and name sibling tests
(`x.go` → `x_test.go`) — a sibling test counts only if it is present in the bead's
evidence; a
declared non-test file is never checked as a test. Declared files present and no
evidence → one gap naming the bead id and saying `no test file in bead evidence`
(RED) or `no implementation file in bead evidence` (GREEN); no declared files and no
evidence → no gap, log `[TDDValidation] <id>: no evidence and no declared targets; side skipped`.
(c) `TDDChainPasses` becomes a wrapper returning `!Located || Passed`. Add
`RunTDDValidationAt(bead, projectPath, evidenceRoot string) error`; `RunTDDValidation`
keeps its signature and wraps it with `evidenceRoot = projectPath` (so
`overseer_chain_validation.go` compiles unchanged). (d) In `bead_closer.go`, the close
path that calls `RunTDDValidation` (~line 326) calls `RunTDDValidationAt` with the
`repoPath` from `resolveEvidenceRepo`, and `verifyTDDChain` resolves the evidence root the
same way and calls `TDDChainVerdict(..., includeDirty=true)`. (e) Replace `hasMeaningfulAssertions` with a
case-sensitive pattern list: `t.Run(`, `func Test`, `t.Error`, `t.Fatal`, `assert.`,
`require.`, `expect(`, `describe(`, `def test_`. Update the two existing core test files
listed in Target Files to the evidence model; update
`docs/operations/bead-validation-system.md` and
`docs/operations/validation-stub-detection.md` (they describe metadata-only
`target_files`) and `CHANGELOG.md` (Fixed). Test functions are named
`TestBeadEvidence_*`, `TestTDDValidator_*`, `TestTDDVerdict_*`.

**Acceptance Criteria:**
- `TestBeadEvidence_*` builds a temp git repo: a commit with subject
  `test: x (hydra-ab12)` touching `pkg/a_test.go` is evidence for `hydra-ab12`; a commit
  whose body has `Bead: hydra-ab12` is too; an unrelated commit is not; a commit mentioning
  `hydra-ab123` is not evidence for `hydra-ab12`; an uncommitted file appears only with
  `includeDirty`, including an untracked file inside a new directory; a non-repo dir yields empty evidence and nil error.
- `TestTDDVerdict_*`: RED bead with empty metadata and `"files": []` whose evidence has a
  test file containing `t.Run(` → `Located && Passed`, zero red gaps.
- RED bead declaring `internal/core/service_starter.go` with no evidence → exactly one
  red gap containing the bead id and `no test file in bead evidence`.
- No evidence and no declared files → `Passed`, zero gaps.
- A Go file containing only `import "testing"` and no assertion pattern is not a
  meaningful test.
- Both halves of the Test Command list `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-004: One strike per attempt, only on new evidence

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/retry_manager.go`, `internal/core/validation_service.go`, `internal/core/validation_rounds_test.go`, `internal/core/overseer_chain_validation_test.go`, `internal/core/validation_retry_test.go`
**Test Command:** `go test ./internal/core/ -run 'TestValidationRound_|TestValidationRetry|TestRunTDDValidation|TestBeadCloser' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-003

Keep `IncrementValidationRetry` / `GetValidationRetryCount` / `ClearValidationRetry`
unchanged as the **verified-close** attempt counter (no call-site churn in test files
this FR does not own). Add separate `TDDRound{Get,Increment,Clear,SetFingerprint,Fingerprint}`
and `PRDRound{Get,Increment,Clear}` methods with their own maps. `RunTDDValidation` and
`RunPRDValidation` use only their round methods; the TDD PASS path clears only the TDD
round; `RetryManager.Clear` clears all three. A TDD round advances only when this pass
created ≥1 fix bead **or** the verdict fingerprint differs from the one recorded at the
previous round; otherwise log `[TDDValidation] no new evidence for <id>; round not advanced`.
Update the strike-accounting section of `docs/architecture/shepherd.md` and
`CHANGELOG.md` (Fixed). Test functions are named `TestValidationRound_*`.

**Acceptance Criteria:**
- First failing pass creates a fix bead → TDD round 1; second failing pass with the same
  fingerprint and dedup skipping creation → still 1; a changed fingerprint → 2.
- `RunTDDValidation` never changes `GetValidationRetryCount`.
- `RetryManager.Clear` zeroes the verified-close count and both round counts.
- Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-005: dep-open requeues without a strike, bounded

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/core/bead_closer_hard_gates.go`, `internal/core/bead_closer.go`, `internal/core/dep_open_requeue_test.go`, `internal/core/startup_migrations.go`, `internal/core/startup_migrations_test.go`, `internal/core/service_starter.go`
**Test Command:** `go test ./internal/core/ -run 'TestDepOpen_|TestStartupMigration_|TestCloseDep_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-004

In `gateDependencyOrder`, a chain-member `dep-open` rejection reopens the bead without
touching the verified-close attempt count and without writing
`verified-close-reject-attempt-N`; it still returns a non-nil error containing
`dep-open` (e.g. `verified-close: bead <id> requeued (dep-open)`) so the close is
aborted. It keeps a durable label `dep-open-requeue-N`
(replaced each round, removed when the dependency gate next passes) and emits
`close_requeued_dep_open` with the open dependency ids. At N ≥ 5 it escalates with
reason `dep-open-stuck`, applying the same label set `rejectClose` uses for escalation
(`needs-human`, `verified-close-escalated`, `escalated`) so Pan's `clear-escalation`
handles it. Add an idempotent startup migration (new `startup_migrations.go`, invoked once
from daemon start in `service_starter.go`; errors are logged and never block startup)
that removes `verified-close-reject-attempt-*` from beads whose only
`verified-close-reject-*` reason label is `verified-close-reject-dep-open` and that are
not `closed-failed`, emitting `migration_dep_open_strikes_cleared` per bead. Add both
events to the event table in `docs/architecture/shepherd.md`; update `CHANGELOG.md`
(Fixed). Test functions are named `TestDepOpen_*` and `TestStartupMigration_*`.

**Acceptance Criteria:**
- Three consecutive dep-open rejections → no `verified-close-reject-attempt-*` label,
  label `dep-open-requeue-3`, no `escalated`, three `close_requeued_dep_open` events.
- Fifth consecutive dep-open → `escalated` + `needs-human` + `verified-close-escalated`,
  reason `dep-open-stuck`.
- A passing dependency gate removes `dep-open-requeue-*`.
- Migration removes the attempt label from a bead with only
  `verified-close-reject-dep-open`; leaves a bead that also has
  `verified-close-reject-tdd-validation-failed`; a second run is a no-op.
- Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-006: TDD validation shadow mode with a readiness signal

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/core/config.go`, `internal/core/validation_service.go`, `internal/core/overseer.go`, `internal/core/validation_mode_test.go`
**Test Command:** `go test ./internal/core/ -run 'TestValidationMode_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-004

Add `validation.tdd_validation_mode: off|shadow|enforce`. Back-compat when unset:
`tdd_validation_enabled: true` → `enforce`, `false` → `off`. Add
`ValidationService.SetEventSink` and wire it in `overseer.go` next to
`o.beadCloser.SetEventSink(events)` (`o.eventSink` is still nil when
`NewValidationService` runs, and a nil sink silently drops events). In `shadow`, `RunTDDValidation` and `TDDChainPasses` compute the
verdict but never reject, never create fix beads and never touch any counter; they log
`[TDDValidation] SHADOW PASS|FAIL <id>: <gap types>` and emit `tdd_validation_shadow`.
Readiness: emit `tdd_shadow_ready` once per process when, in a rolling 24h window, there
are ≥10 shadow verdicts on located chains, ≥1 PASS, and a FAIL rate ≤ 20%. Document the
mode, both events and the shadow→enforce rollout step in
`docs/operations/bead-validation-system.md`; update `CHANGELOG.md` (Added). Test
functions are named `TestValidationMode_*`.

**Acceptance Criteria:**
- Table test: unset mode with `tdd_validation_enabled` true/false → enforce/off.
- Shadow + failing chain → `TDDChainPasses` true, no fix bead, all round/attempt counts
  unchanged, one `tdd_validation_shadow` event with verdict `FAIL`.
- Enforce + failing chain → `TDDChainPasses` false.
- Readiness: 9 verdicts → no event; the 10th qualifying verdict → exactly one
  `tdd_shadow_ready`; a later verdict → no second event.
- Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-007: Deterministic TDD fix-bead sweep, plus the clear-escalation guard

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/core/pan_tdd_fix_sweep.go`, `internal/core/pan_tdd_fix_sweep_test.go`, `internal/core/fix_audit.go`, `internal/core/overseer.go`, `internal/core/service_starter.go`, `internal/core/clear_escalation_test.go`
**Test Command:** `go test ./internal/core/ -run 'TestTDDFixSweep_|TestClearEscalation_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-005, FR-006

(a) Add a deterministic `sweepTDDFixBeads` (new file; not routed through Pan's LLM
FIX-AUDIT, whose prompt assumes the original closed as failed; `fix_audit.go` `BuildFixBeads` is
changed to exclude beads labelled `tdd-validate-round-*`, which today still reach it when
their type is `fix`). Candidates: open beads labelled `auto-fix`
+ `tdd-validate-round-*` (any type). For each, read the original from `Original bead: <id>`
and call `TDDChainVerdict` with `includeDirty=false` (the main checkout's dirty files are
not evidence for a non-closing bead); close the fix bead with label `premise-cleared` and event
`autofix_premise_cleared` only when `Located && Passed`, or when the original is
terminal and not `closed-failed`. Leave untouched when mode is `off`, when the verdict
is not located, or when a live worker holds the fix bead. The sweep has a
`liveWorker func(beadID string) bool` field defaulting to
`o.workerSpawner.HasLiveRegistryWorker`; at most one close per project per run; wired
on the stale-filing tick in `overseer.go`; fail-open. (b) Extract the `clear-escalation`
closure into a named method and make its refusal check the **bead's** labels (today
`!escLabels[escalatedLabel]` tests the static map); behavior otherwise unchanged (the
one-fair-retry contract is preserved). Update `docs/architecture/shepherd.md` and
`CHANGELOG.md` (Fixed). Test functions are named `TestTDDFixSweep_*` and
`TestClearEscalation_*`.

**Acceptance Criteria:**
- Original passes (`Located && Passed`) → fix bead closed with `premise-cleared`, one
  event; a second candidate in the same project waits for the next run.
- Shadow mode with a failing original → untouched (the verdict, not `TDDChainPasses`,
  decides).
- Original `closed-failed` → untouched; not located → untouched; mode `off` → untouched;
  live worker → untouched.
- `clear-escalation` on a bead without `escalated` returns an error containing
  `carries no "escalated" label` and changes no labels; with `escalated` it behaves as today.
- Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

## Success Metrics

- After deploy in `shadow`: `tdd_shadow_ready` within 24h on real hydra chains; then
  `enforce` with zero false gaps over the following week.
- Zero escalations whose only reason is `dep-open`.
- Zero manually closed `auto-fix` beads in the week after deploy.

## Out of Scope

- Adopting `beadfiles.Resolve` in `PRDValidator` (live behavior change while
  `prd_validation_enabled: true`), `beadTargetFiles` / ScopeGate / target-overlap /
  prespawn / satisfied-close — PRD-6b, each with sibling-test scoping and its own tests.
- `validator.go` `IsEndOfChain` and agent heuristic, `compliance_gate.go`,
  `bead_analyzer.go`, `hindsight/types.go` extractors.
- Campaign-wide park (label-based sweep, `br list --all`, BeadsClient change) — PRD-6b.
- Making workers always write a `Bead:` trailer on their own commits — PRD-6b
  (evidence matching on the id string covers today's commits).
- Runaway-process defense — PRD-5. Hard-gate infra classification and deps preflight — PRD-6b.
