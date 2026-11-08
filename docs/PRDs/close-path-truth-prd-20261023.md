# PRD: Close-Path Truth & Ordering — Infra-Classified Gates, Deps Preflight, Dependent-Release Rollback, Decision Packets

**Version:** 1.0 · **Date:** 2026-10-23 · **System Type:** library · **Repo:** components/hydra

## Problem

Three verified incidents show the close path cannot distinguish "the work failed"
from "hydra could not measure the work", and releases dependents on work that was
never accepted:

1. **Env failures burn verified-close strikes.** `gateExecutableAcceptance`
   (`internal/core/bead_closer_hard_gates.go:133`) calls `rejectViaGate`
   (`"verification-failed"`) directly, bypassing the gate-doctor triage that
   `handleVerificationRed` (`bead_closer.go:417`) provides. srv-data-harbour
   `yxo` burned 3/3 strikes on `sh: 1: jest: not found` (exit 127, fresh worktree
   without `node_modules`) and on `/bin/sh: 1: Cannot fork` (exit 2, fork bomb,
   2026-10-22 11:30:16). Neither says anything about the bead's work. The wired
   gate-doctor (`GateDoctor.Triage`, `gate_doctor.go:156`) already classifies
   environment vs code-broken but is never reached from this gate.
2. **Gates run in unprepared worktrees.** A recycled chain worktree is recreated
   in seconds with no dependencies installed; the close gate then executes the
   bead's verification command there. Verified for `pbu` on 2026-10-22 (three
   exit-127 rejections at 05:51, 14:33, 15:47; `node_modules` appeared only
   3 s after the last reject, from the next worker's own `pnpm install`).
   `hydra-4md` (sealed-snapshot node_modules preflight, open since 2026-09-24)
   never landed.
3. **Dependents are released before the close is accepted, and never re-blocked.**
   `worker_lifecycle.go:276` logs `[DAG] %d dependents now ready after %s
   completion` when the **worker exits**; the verified-close rejection that
   follows does not roll that back. Verified 2026-10-22 14:33:08-13: yxo's
   worker exited → DAG released `yse` → yxo's gate rejected 3/3 (escalated) →
   `yse` spawned anyway 1 s later, ran, and closed 17:02 with
   `close_dep_open warn … (singleton, closing anyway)` and a daemon-side
   `--force` past br's own "open children" refusal. Worker-success ≠
   close-success; today nothing encodes that.
4. **Spec-infeasible gates have no exit but noise.** When a gate fails
   deterministically because an acceptance criterion cannot be met by the code
   (yxo: per-path coverage ≥80 while 15/17 modules measure below 80 at
   **aggregate** semantics — llm 36.1/4.6/8.7/36.1, event-feed 45.8, master-data
   54.4; measured from `coverage/lcov.info` 2026-10-23), the correct behavior —
   the spec's own "STOP and escalate" — produces an hourly
   clear-escalation/respawn loop (16 `orphan_escalated_wedged` events on
   2026-10-23 alone) and a human triages from raw logs instead of from a packet
   of measured facts.

Related, deferred from PRD-6a review rounds: campaign-wide park (label-based) and
`beadfiles.Resolve` adoption in the closer were explicitly Out-of-Scope there.

## Verified Facts (2026-10-22/23)

- yxo CloseGate rejections: 3× exit 127 `jest: not found`, 1× exit 2
  `Cannot fork`, 2× exit 1 genuine threshold breaches (3137/3137 tests pass).
- Fresh `pbu` worktree at gate time: `node_modules` absent (ls 2026-10-23 03:51).
- `yse` spawn at 14:33:13, one second after yxo's escalation at 14:33:12;
  `close_dep_open warn … singleton` at 17:00:38; `bead_closed` 17:02:21 with
  `[CloseBead] … has open children; retrying close with --force`.
- Retry-queue spawn path already consults open dependencies
  (`scheduler.go` ~648-672, "Skipping retry … blocked by N open dependencies"),
  so the leak is the DAG release at worker completion, not the retry queue.
- Gate-doctor exists and is wired (`overseer.go:623
  SetGateDoctor(NewGateDoctor(...))`) but only `verificationCommandGuard`
  routes through it.
- Coverage measurements (lcov, campaign branch `feat/unit-test-coverage-rollout`
  @ c9f6364f): 584 file-level threshold breaches across 17 per-path glob blocks;
  module aggregates ≥80 in only 2/17 (`task-feed`, `document-meta-data`).

## Goals

- A gate rejection means the work failed, not that hydra could not run a command.
- Verification commands execute in a worktree that can actually run them.
- Dependent readiness tracks close acceptance, not worker exit.
- Deterministic spec-infeasibility surfaces as a measured decision packet for a
  human, and stops burning worker runs.

### FR-001: Infra classification at the executable-acceptance gate

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/bead_closer_hard_gates.go`, `internal/core/gate_infra_class.go`, `internal/core/gate_infra_class_test.go`
**Test Command:** `go test ./internal/core/ -run 'TestGateInfra_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none in this PRD. Sequencing precondition: the dep-open chain of PRD `validator-truth-prd-20261022.md` must land first (it edits `gateDependencyOrder` in the same file); this requirement edits only `gateExecutableAcceptance` (:106-135) and new files

Add `classifyGateFailure(exitCode int, verbatim string) GateFailureClass` in a new
file with classes `infra` and `work`. `infra` when: exit 127, exit 2 with `Cannot fork`, or a matched diagnostic that
appears on a shell/toolchain line (`sh: 1:`, `/bin/sh:`, `npm error`, `ENOENT`/
`EACCES` as the errno line) **before the first project output line**. A
`Cannot find module '<x>'` line inside jest/vitest output is `work` (bad import
= code), even though it contains "not found". `work` otherwise. In
`gateExecutableAcceptance`, replace the direct `rejectViaGate(...,
"verification-failed", ...)` with: class `infra` → emit `close_gate_infra`
(verbatim evidence) and return a rejection whose reason is `infra-failure`;
class `work` → today's `verification-failed` path. The infra path bypasses
`rejectClose` entirely — no retry-manager increment, no fingerprint write, no
`verified-close-reject-attempt-N`. Infra rejections keep a durable label
`infra-requeue-N` (replaced each round; removed on any green gate or
work-class rejection; cap read via `durableInfraRequeues(labels)`). "Consecutive"
means since the last non-infra close-gate outcome. At N > 3 escalate with
reason `infra-stuck` using the label set `needs-human`+`verified-close-escalated`+
`escalated` (mirrors `isInfrastructureExitCode` semantics at worker_lifecycle.go:186). Update the event table in
`docs/architecture/shepherd.md` and `CHANGELOG.md` (Fixed). Test functions are
named `TestGateInfra_*`.

**Acceptance Criteria:**
- Table tests: exit 127 + `sh: 1: jest: not found` → infra; exit 0-project-output
  then threshold breach text → work; exit 2 + `Cannot fork` → infra; exit 1 with
  jest summary output → work; exit 1 with `Cannot find module 'leftpad'` inside
  jest output → **work**; `npm error ENOENT` before project output → infra.
- Gate test with a fake command returning 127: bead reopens with reason
  `infra-failure`, no `verified-close-reject-attempt-*` label, one
  `close_gate_infra` event.
- Fourth consecutive infra rejection → escalated, reason `infra-stuck`, label
  set `needs-human`+`verified-close-escalated`+`escalated`. Infra rejection → no
  retry-manager increment, no fingerprint write, no reject-attempt label; a
  green gate removes `infra-requeue-*`.
- The Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-002: Deps preflight (companion to FR-001, same chain)

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/gate_deps_preflight.go`, `internal/core/gate_deps_preflight_test.go`, `docs/operations/host-setup.md` (the one-line call site inside `gateExecutableAcceptance` belongs to the first requirement's edit budget)
**Test Command:** `go test ./internal/core/ -run 'TestGateDeps_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none (executes in the same TDD chain as FR-001; the overlap rule forbids two requirements editing `bead_closer_hard_gates.go`, so requirement one wires both behaviors into `gateExecutableAcceptance`: classification and the preflight call)

Add `gateDepsPreflight(dir, manifest string, run Runner) (ok bool, ran bool)`
(with `Runner func(cmd string, dir string) error` injected so tests fake the
installer): when
`dir` lacks the manifest's dependency directory (`node_modules` for
`package.json`, `target` marker for `cargo`, `.venv`/`site-packages` for
`requirements.txt`/`pyproject.toml`), run the project's install command detected from lockfiles:
`pnpm install --prefer-offline --frozen-lockfile` / `npm ci --ignore-scripts` /
`cargo fetch` (python is skipped: `pip install -e .` executes worker-authored
build hooks as the hydra uid — not allowed). Presence markers: `node_modules`
for npm/pnpm; for cargo use `Cargo.lock` + `~/.cargo/registry` freshness —
`cargo fetch` never creates `target/`, so `target/` is NOT a marker. A
per-directory singleflight mutex serializes installs into one shared chain
worktree. with a hard 10-minute timeout and a lockfile-guard
(the worktree tree must be clean after; installed dirs are gitignored). Wire it
into `gateExecutableAcceptance` before `exec.Command`: if the install fails or
times out, reject with reason `infra-failure` (FR-001 class) and evidence
`deps preflight failed: <err>`; never run the verification command against a
missing dependency dir. Log line `[GateDeps] <bead> preflight <mode>` (mode =
`present` | `installed` | `failed`). Fail-open policy: unknown project types
(no manifest) skip the preflight. Update `docs/operations/host-setup.md`
(disk/cadence note) and `CHANGELOG.md` (Added). Test functions are named
`TestGateDeps_*`.

**Acceptance Criteria:**
- Unit tests with temp dirs: missing `node_modules` + `package.json` → install
  invoked via the fake Runner; install failure → rejection reason
  `infra-failure` with `deps preflight failed:` evidence; present deps →
  `present`, no install; concurrent calls for one dir run the installer once
  (mutex).
- No manifest → skipped, verification command runs.
- Install that dirties tracked files → treated as failure (tree-clean guard).
- The Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-003: Dependent-release rollback on rejected close

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/worker_lifecycle.go`, `internal/core/build_dependency_graph.go`, `internal/core/dag_rollback_test.go`
**Test Command:** `go test ./internal/core/ -run 'TestDagRollback_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none

On a verified-close rejection (any reason) or escalation for bead B, revoke the
readiness B's worker-completion granted. The session `completed`-map deletion
already exists (`worker_lifecycle.go` ~235-239, gybo4) — assert it, do not
duplicate. The missing half is the DAG: `HandleSuccess` → `dag.MarkCompleted`
(`worker_lifecycle.go:269`, `build_dependency_graph.go:182`) decrements
dependents' `InDegree` before the close gates run. Add
`DependencyGraph.RequeueBead(beadID)` restoring the node status and dependents'
`InDegree`; call it from the close-error path and run it **before** the
`triggerSchedule` at `worker_lifecycle.go:394`, then re-read `newlyReady` so
dependents whose only satisfier was B lose readiness. Emit
`dag_dependent_release_revoked` listing revoked dependent ids. Idempotent,
fail-open. Scope: rejected/escalated closes only; the singleton warn-close path
is a named follow-up (Out-of-Scope) — the in-flight race window (dependent
spawned seconds after rejection) shrinks to the pre-rollback interval, which
the regression test must measure as ≤ one schedule tick. Add a regression test that reproduces the 2026-10-22
14:33 sequence. Update `docs/architecture/shepherd.md` and `CHANGELOG.md`
(Fixed). Test functions are named `TestDagRollback_*`.

**Acceptance Criteria:**
- Test: worker completion marks B completed and releases dependent D; a
  subsequent verified-close rejection for B revokes D's readiness (D not in
  ready set; event lists D).
- Test: rejection of a bead with no released dependents emits no event.
- Test: rollback is idempotent (second rejection emits nothing new).
- Test: a bead that closes verified keeps its dependents released.
- The Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-004: Spec-infeasible decision packets

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/core/pan_decision_packet.go`, `internal/core/pan_decision_packet_test.go`, `internal/core/pan_stale_filing.go`, `internal/core/bead_closer.go`, `internal/core/service_starter.go`, `docs/architecture/shepherd.md`
**Test Command:** `go test ./internal/core/ -run 'TestDecisionPacket_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-001

Hang `decisionPacketCheck` off the existing stale-filing sweep body
(`pan_stale_filing.go`; the tick is invoked once from overseer). First make the
fingerprint count durable: `trackVerificationFingerprint` (`bead_closer.go`
~1848-1867) stores `gate-fp-<fp8>-<N>` (N replaced each round; the fingerprint
hashes beadID+cmd+reason, so all `work` rejections of a bead share it by
construction — the criterion reduces to "N≥3 work rejections", stated). Then: a
bead with `gate-fp-*-N` where N≥3, no `infra-requeue-*` since, and whose latest
worker verdict was `done:false` (durable verdict log, `ExtractRecordedVerdict`,
`bead_closer.go:877`) is **spec-infeasible** (also in this FR: move `beadTargetFiles` and `splitFileList` from `bead_closer.go` ~1934 verbatim into the new `internal/core/beadfiles_adoption.go` — pure relocation, behavior unchanged, freeing FR-006 from touching `bead_closer.go`):
label `spec-infeasible`; make the clear-escalation handler
(`service_starter.go` ~477-500) refuse with `clear-escalation: <id> is
spec-infeasible; awaiting spec-decided` when that label is present; and emit
`decision_request` with a payload
containing bead id, the verification command, the rejection fingerprint, the
last verbatim gate output (truncated to 4 KB), and a `measurements` map the
sweep fills from the verbatim output (for jest: the `All files` row and the
per-path breach count). A human decision (label `spec-decided:<choice>` or a
status change) un-parks it. Update `docs/architecture/shepherd.md` (sweep,
label, event) and `CHANGELOG.md` (Added). Test functions are named
`TestDecisionPacket_*`.

**Acceptance Criteria:**
- Test: 3 same-fingerprint `work` rejections + `done:false` verdict →
  `spec-infeasible` label, one `decision_request` event whose payload contains
  the command and ≥1 measurement; bead excluded from clear-escalation
  candidates.
- Test: infra-class rejections never trigger the packet.
- Test: fewer than 3 matching fingerprints → untouched.
- Test: labeling `spec-decided:<x>` removes `spec-infeasible` candidacy.
- The Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-005: Campaign-wide park via label sweep

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/core/campaign_park.go`, `internal/core/campaign_park_test.go`, `internal/core/dag_navigator.go`, `internal/core/scheduler.go`, `internal/core/overseer.go` (BeadsClient interface at :67)
**Test Command:** `go test ./internal/core/ -run 'TestCampaignPark_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none

A throttled (per stale-filing tick), fail-open sweep reads beads with
`br list --json --all --limit 0` (ensure the listing uses `br list --json --all --limit 0`
(add `ListAllBeads(projectPath)` to `BeadsClient` at `overseer.go:67` if the
existing `ListBeads` omits terminal beads; update ALL mock implementations per
AGENTS.md — `mockBeadsClient` in `overseer_integration_test.go`,
`enhancedMockBeadsClient` in `worker_completion_test.go`). Membership: nearest
`APPROVE:`-titled ancestor by BFS over dependent edges including through
terminal beads; BFS stops at any other `APPROVE:`/`PARK-GATE:`/HOLD node; a
bead reachable from two APPROVE ancestors belongs to neither (log). While any
open `PARK-GATE:` bead exists for campaign C, stamp label `campaign-parked` on
C's members — idempotently: only members whose current label set (already in
the listing payload) lacks it, so steady state performs zero `br` writes; when
the gate closes, remove it. BFS walks the raw `Dependencies` arrays from the
listing (never the in-memory DAG, whose edges skip terminal deps), with a
visited set for cycle safety. `GetReadyBeads`
(`dag_navigator.go:89`) and the scheduler retry path
(`scheduler.go` ~648-672) skip beads labelled `campaign-parked`; an in-progress
member emits `campaign_park_leak` once. Update mocks, `docs/architecture/
shepherd.md` (PARK-GATE vs HOLD conventions), and `CHANGELOG.md` (Fixed). Test
functions are named `TestCampaignPark_*`.

**Acceptance Criteria:**
- Fixture reproducing the hydra-gcip leak (gate attached only to an unrelated
  member; another member's upstream already terminal): the member is excluded
  from ready beads and from retry-queue eligibility.
- Test: a campaign-2 bead depending on a campaign-1 member is not parked by a
  campaign-1 gate (BFS stop).
- Test: closing the PARK-GATE removes labels and restores readiness.
- Test: in-progress member emits exactly one `campaign_park_leak`.
- `go build ./...` and `go vet ./internal/core/` exit 0; the Test Command
  output lists `--- PASS` lines and no `[no tests to run]`.

### FR-006: Adopt the shared resolver in the close path (sibling-test scoping)

**Type:** fr
**Priority:** P2
**System Type:** library
**Target Files:** `internal/core/beadfiles_adoption.go`, `internal/core/scope_gate.go`, `internal/core/beadfiles_adoption_test.go`
**Test Command:** `go test ./internal/core/ -run 'TestBeadfilesAdoption_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none within this PRD; sequencing precondition: the
`internal/beadfiles` package from PRD-6a FR-001 must exist on the branch (chain
runs last).

Replace the relocated `beadTargetFiles` body in `beadfiles_adoption.go` with
`beadfiles.Resolve` **plus** one-directional sibling expansion: for each resolved impl file `x.go`
include `x_test.go` as in-scope. A bead declaring only test files gains NO
impl-file scope — such a diff stays empty-diff and belongs to the GREEN bead. Callers whose behavior
changes (`hasTargetChanges` scope match at `scope_gate.go:139`,
`prespawn_probe.go:43`, `target_overlap.go:75,86`, `satisfied_close.go:93`,
`fix_audit.go:138`, `integration_retest.go:678`, `gate_close.go:75`) each get
one regression test asserting the new expected verdict (RED bead whose worker
wrote only `service_starter_test.go` now closes, not `empty-diff`). Update
`CHANGELOG.md` (Changed). Test functions are named `TestBeadfilesAdoption_*`.

**Acceptance Criteria:**
- Sibling test: bead declares `internal/core/service_starter.go`; diff contains
  only `internal/core/service_starter_test.go` → in-scope (no empty-diff).
- One regression test per caller listed above, asserting its verdict.
- `go build ./...` exits 0; the Test Command output lists `--- PASS` lines and
  no `[no tests to run]`.

### FR-007: prd-lint coverage-threshold semantics rule

**Type:** fr
**Priority:** P2
**System Type:** library
**Target Files:** `tools/prd-lint/main.go`, `tools/prd-lint/main_test.go`, `CHANGELOG.md`
**Test Command:** `go test -C tools/prd-lint ./... -count=1 -v`
**Build Command:** `go build -C tools/prd-lint ./...` (separate Go module;
plain `go build ./...` from the repo root never builds it)
**Depends On:** none

Add a lint rule: when a PRD's acceptance criteria mention
`coverageThreshold`/`per-path`/`test:cov` thresholds with glob-style keys
(`**`), require an explicit `**per-file: true**` or `**aggregate: true**` marker
in the same criterion; missing marker → warning `R10-coverage-semantics` (R8 is taken)
(jest glob keys apply per file; directory keys aggregate — srv-data-harbour
2026-10-23: 584 per-file breaches while 15/17 module aggregates also below
target). Warning-level (not error) so existing PRDs lint clean. Test functions
named `TestLint_CoverageSemantics*`. Update `CHANGELOG.md` (Added).

**Acceptance Criteria:**
- Fixture PRD with a glob threshold criterion and no marker → warning contains
  `R8-coverage-semantics`; with `**aggregate: true**` → clean; with
  `**per-file: true**` → clean.
- Rules without coverage mentions are unaffected (regression fixture).
- `go build ./...` exits 0; the Test Command output lists `--- PASS` lines and
  no `[no tests to run]`.

## Success Metrics

- Zero escalations whose reason is an infra failure (exit 127/fork/OOM) after
  deploy; `close_gate_infra` events with a following successful verified close.
- No dependent runs while its dependency's close is rejected (regression test
  + live: zero `campaign_park_leak`-style reorderings of chain gates).
- yxo-class loops produce exactly one `decision_request` and zero respawns
  per day.
- Fresh-worktree gates show `[GateDeps] … preflight installed` at least once
  per recycled chain before a green gate.

## Out of Scope

- Writing the missing srv-data-harbour tests (product work, its own campaign).
- Threshold re-basing decisions for harbour (human; measured table supplied
  2026-10-23).
- Session-per-attempt / oversized-session quarantine (separate PRD, "Session &
  Env Hygiene").
- Singleton warn-close dependent release (the `close_dep_open warn-only` path at
  `bead_closer_hard_gates.go:188-193`): FR-003 scopes to rejected/escalated
closes; the singleton path needs its own deferral design (yse evidence
  2026-10-22 17:00).
- Runaway-process defense (PRD-5).
