# PRD: Executable Acceptance & Post-Close Verification

**Date:** 2026-10-02
**Status:** Draft
**Author:** operator + fleet analysis session
**Related:** `task-breakdown-pattern-routing-20261002.md`, shepherd event-stream contract, beads `hydra-mmkn` (sensor symmetry), `hydra-v1s3` (DAG edge integrity)

## 0. Scope Profile

**System Type:** feature

**Summary:** Close the "closed ≠ acceptance met" gap. Every close path currently executes gates (lint/build/test) and an LLM judge round, but no layer executes the bead's own acceptance against reality. The PRD contract already defines a per-requirement Test Command; the Go close path never runs it (zero references in `internal/core`/`internal/validation`). This PRD makes acceptance executable, runs it at close time, re-verifies after landing, and bounds the auto-reopen loop.

## 1. Background and Evidence

Measured 2026-10-02 on the production daemon (`/var/log/hydra/hydra-error.log`, window 00:17–13:20 UTC):

- CloseEvent histogram: **192 accepted / 27 rejected / 11 escalated**. Rejection reasons: `tdd-validation-failed`×24, `empty-diff`×6, `gate-test-failed`×4, `gate-build-failed`×3, `gate-lint-failed`×1. The existing layers (gates, empty-diff guard, judge) demonstrably work.
- Escape example: `hydra-evzr` closed "completed" at 10:51 with non-empty diff and green gates while its acceptance (fms PRD compiles to direct_task beads, zero TDD chains) was measurably unmet. Operator re-test found 25 beads / 13 chain beads / 0 direct_task beads. Reopened with evidence; the retry's close was then correctly rejected (`empty-diff`, round 1/3) — the machinery catches *no work*, not *wrong work*.
- `hydra-wiu6` closed while its GREEN worktree sat quarantined under `hydra/quarantine/hydra-wiu6` — a close-vs-quarantine contradiction with no detector.
- SYNTAX beads (`9oqj`, `jwdi`, `96l2`) closed while their chain GREENs were still unfinished — no close-time dependency check.
- fms campaign historically: 42 clean closes against 58 unmerged PRs — closed-but-unlanded was invisible until manual audit.
- Prior art: `internal/core/bead_closer.go:1215` references the 2026-09-16 incident (14 false closes) that added the current acceptance check; `internal/core/satisfied_close.go` already fail-closes on missing gates and distrusts green suites without bead-level evidence.

## 2. Requirements

### FR-001: Verification Command contract and propagation

**Type:** fr
**Priority:** P0

**Target Files:** `.agents/skills/task-breakdown/runner.mjs`

**Test Command:**
```
node .agents/skills/task-breakdown/test-verification-command.mjs
```

**Build Command:**
```
node --check .agents/skills/task-breakdown/runner.mjs
```

Add a `verification_command` field to the bead contract. `task-breakdown` MUST populate it deterministically from the PRD's per-requirement Test Command (never from worker free text), for every emitted bead regardless of pattern (tdd_chain, direct_task, configuration_task, aggregation_task). Beads synthesized without a source command receive the requirement's Build Command plus a non-vacuity wrapper. The field is stored in the bead description in a machine-parseable form (`**Verification Command:**` fenced block) so the Go closer can extract it without parsing prose.

**Acceptance Criteria:**
- `test-verification-command.mjs` proves: every fixture PRD's beads carry a verification_command; the command string matches the requirement's Test Command byte-for-byte; beads for requirements lacking Test Command carry Build Command plus non-vacuity wrapper
- A fixture bead whose PRD Test Command is missing/empty still compiles, with the wrapper present
- Suite exits 0

---

### FR-002: Close-time hard gates in the bead closer

**Type:** fr
**Priority:** P0

**Target Files:** `internal/core/bead_closer.go`

**Test Command:**
```
go test ./internal/core/ -run 'TestVerifiedClose|TestCloseGate|TestCloseDep|TestQuarantineClose' -count=1
```

**Build Command:**
```
go build ./internal/core/
```

Extend VerifiedClose with three hard gates that run before the LLM judge round:

1. **Executable acceptance gate**: if the bead carries a verification_command, execute it (via the FR-003 runner) in the bead's worktree; non-zero exit rejects the close with the command output attached verbatim to the rejection event. Absence of a command never blocks close (fail-open for legacy beads) but emits a `verification_command_missing` typed event.
2. **Quarantine contradiction gate**: a close is rejected when the bead's worktree branch was quarantined and never integrated (`hydra/quarantine/<bead>` exists and the bead has no integration record since). Rejection reason `closed-vs-quarantined`.
3. **Dependency-order gate**: closing a bead while any of its `blocks`-edge targets are non-terminal emits `close_dep_open` and rejects with reason `dep-open` when the bead is a chain member (RED/GREEN/REFACTOR/SYNTAX). Singletons warn-only, because approval-gate and operator-closed beads legitimately close with open children.

**Acceptance Criteria:**
- Unit tests per gate: reject path, pass path, fail-open legacy path
- Rejection events carry the verbatim command output or the quarantine/dep evidence
- Existing VerifiedClose round/escalation semantics unchanged (rejected → reopen → attempt cap → escalate)

---

### FR-003: Sandboxed verification runner with provenance and non-vacuity

**Type:** fr
**Priority:** P0

**Target Files:** `internal/core/verification_runner.go`

**Test Command:**
```
go test ./internal/core/ -run 'TestVerificationRunner' -count=1
```

**Build Command:**
```
go build ./internal/core/
```

New component that executes verification commands safely. Commands are LLM-influenced text, so execution is a security boundary:

- **Provenance**: only commands carried in the bead's machine-parseable field (FR-001) or from a per-bead-type trusted template registry execute; free-text found elsewhere in the description is ignored.
- **Sandbox**: runs as the hydra user inside the bead's worktree, 120s timeout, no network namespace where the platform allows, environment scrubbed of credentials (`GH_TOKEN`, `GITHUB_TOKEN`, `*_PAT`, `AWS_*`, `OPENROUTER_*`).
- **Non-vacuity**: a command that exits 0 while producing zero assertion evidence (no test-count line, no file-existence proof, empty stdout) is treated as failure with reason `vacuous-verification`. Wrapper commands from FR-001 embed an assertion marker to satisfy this.
- All executions emit typed events (`verification_run` with exit code, duration, vacuity flag) — fail-open: runner errors reject the gate attempt but never crash the closer.

**Acceptance Criteria:**
- Unit tests: injection string (`; rm -rf /` class) never reaches a shell metacharacter layer (command runs via argv, not `sh -c`); credential env vars absent in child; timeout kills; vacuous zero-output pass is rejected
- Events emitted on both pass and fail paths

---

### FR-004: Post-close verification sweep with flake guard and bounded auto-reopen

**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/postclose_sweep.go`

**Test Command:**
```
go test ./internal/core/ -run 'TestPostcloseSweep' -count=1
```

**Build Command:**
```
go build ./internal/core/
```

A daemon sweep (15-minute cadence, same shape as the rrtq aging sweep) that re-runs the verification_command of recently-closed beads against a fresh checkout of the project's default branch — catching integration skew that the worktree gate cannot. Behaviour:

- Only beads closed within the last 24h that carry a verification_command are swept; each bead swept at most once.
- Failure runs the command a second time after 60s (flake guard, same double-breach pattern as the disk watermark design). Two consecutive failures → auto-reopen with both outputs attached as a comment, label `verification-regression`, typed event `postclose_reopen`.
- Inconsistent results (fail then pass) → label `flaky-verification`, no reopen, typed event for human review.
- Auto-reopen bounded: a bead reopened by the sweep twice carries `needs-human` and is never auto-reopened again (escalation, matching the existing attempt-cap doctrine).
- Sweep errors (checkout failure, runner error) fail open with telemetry.

**Acceptance Criteria:**
- Unit tests: reopen path attaches both outputs; flake path labels without reopening; second reopen adds `needs-human`; sweep skips beads older than 24h and beads without commands
- Integration test: closed fixture bead whose command fails on the default branch is reopened with the label and comment

---

### FR-005: Dangling-work detector

**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/stranded_work.go`

**Test Command:**
```
go test ./internal/core/ -run 'TestStrandedWork|TestDanglingWork' -count=1
```

**Build Command:**
```
go build ./internal/core/
```

Extend the existing stranded-work detection into a close-time audit: for every bead transitioning to closed, verify at least one of (a) integration record, (b) an open or merged PR referencing the bead, (c) an explicit operator close reason. None present → typed event `dangling_close` with the bead's worktree/quarantine state, and the bead is labeled `dangling-work` for the operator digest. This converts the fms failure mode (42 closes / 58 unmerged PRs, discovered by hand) into a continuously measured quantity. Detector only: it never reopens by itself in this PRD.

**Acceptance Criteria:**
- Unit tests for each evidence class and the none-present path
- Event payload includes quarantine branch name when present
- Daily count queryable from the event stream (SQL or script shown in PR description)

---

### Doc-001: Producer contract documentation

**Type:** docs
**Priority:** P1

**Target Files:** `.agents/skills/create-prd/SKILL.md`

**Test Command:**
```
grep -c 'Verification Command' .agents/skills/create-prd/SKILL.md && grep -c 'post-close' docs/architecture/shepherd.md
```

**Build Command:**
```
test -s .agents/skills/create-prd/SKILL.md
```

Document the `**Verification Command:**` field in the create-prd contract: when authors must provide it (always, for FRs), how task-breakdown propagates it, the provenance rule (PRD-authored or template, never worker-invented), and the non-vacuity requirement with examples of vacuous vs real commands. Update the shepherd architecture doc's close-path description to include the three new hard gates and the post-close sweep.

**Acceptance Criteria:**
- SKILL.md contains the field specification and at least one vacuous/real command pair example
- `docs/architecture/shepherd.md` close-path section lists the gates and sweep

---

### NFR-001: Escape-rate regression budget

**Type:** nfr-verification
**Priority:** P1

**Target Files:** `internal/core/postclose_sweep.go`

**Test Command:**
```
go test ./internal/core/ -run 'TestPostcloseSweep' -count=1
```

**Build Command:**
```
go build ./internal/core/
```

Telemetry baseline from 2026-10-02: 192 accepted / 27 rejected / 11 escalated closes; one confirmed semantic escape (`hydra-evzr`). After rollout, the event stream must support a weekly query computing: closes with verification_command present, gate rejections by reason, postclose reopens, dangling closes. Target steady state: zero `evzr`-class escapes on the fixture corpus (a bead whose verification_command fails cannot close), and `verification_command_missing` events trending to zero for new beads once producers adopt FR-001.

**Acceptance Criteria:**
- The weekly query (SQL or script) is demonstrated in the PR description with real output
- Fixture: a bead whose verification_command exits 1 cannot reach closed-completed through any close path (verified-close, satisfied-close, singleton integrate)

---

### NFR-VALID: Validation Gate

**Type:** nfr-verification
**Priority:** P1

**Target Files:** `internal/core/bead_closer.go`

**Test Command:**
```
go test ./internal/core/... -count=1
```

**Build Command:**
```
go build ./... && go vet ./...
```

Full core test suite green, vet clean, and the flake guard demonstrated by a unit test that injects one transient failure followed by a pass and asserts no reopen occurs.

**Acceptance Criteria:**
- `go test ./internal/core/...` exits 0
- `go vet ./...` exits 0
- Flake-guard unit test present and passing

## 3. Non-Goals

- Auto-closing of dangling work (detector-only this round; closing is a human/Pan decision)
- Retroactive sweeping of beads closed before rollout
- Changing LLM judge prompts or max_rounds (telemetry from `hydra-mmkn` lands first; tuning follows evidence)
- Trusting worker-authored verification commands (explicitly excluded by the provenance rule)

## 4. Interactions and Risks

- **`hydra-mmkn` (sensor symmetry)**: this PRD's events follow the fire+resolve convention it defines; landing order flexible, but the weekly query in NFR-001 assumes mmkn's event stream shape.
- **`hydra-v1s3` (DAG edge integrity)**: FR-002's dependency-order gate consumes edge state; v1s3's pruning must not hide non-terminal blockers from the gate. Pruning targets terminal/missing nodes only.
- **Charon/OrphanRetry**: sweep-triggered reopens must register with the same orphan-claim machinery or the reopened bead respawns into contention (existing hazard noted at `bead_closer.go:537`). FR-004 acceptance includes an integration test proving the reopened bead flows through the normal dispatch path.
- **Cost**: the sweep executes one command per qualifying close; bounded by the 24h window and once-per-bead rule. Gate execution adds one bounded subprocess per close attempt.
- **Security**: FR-003 is the load-bearing control. Any relaxation of provenance or sandboxing is a separate PRD with a security review.
