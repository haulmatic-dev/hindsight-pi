# PRD: Typed Workflow Templates and Daemon-Owned Deterministic Gates

**Status:** Draft (planning-complete, pending approval)
**Date:** 2026-08-13
**Origin:** srv-smart-engine campaign analysis + external research review

---

## 1. Overview

**Problem:** Every bead type runs the same RED→GREEN→REFACTOR→SYNTAX chain plus
five overlapping verification layers, so test-only and meta work burn full LLM
spawns on deterministic checks while validators disagree and spawn churn beads.

**Solution:** Bead-type-keyed workflow templates; deterministic checks move from
LLM beads into daemon-owned fail-closed gates; verification consolidates to one
calibrated adversarial validator.

**Success Metrics:**
- Tokens per closed bead reduced ≥40% for `test-coverage` template vs legacy chain (registry `tokens_so_far`, wall-clock `completed_at - started_at`)
- Machinery-bead ratio (auto-fix/re-validate ÷ designed beads) reduced from ~5:1 (srv-smart-engine: ~300:58) to ≤0.5:1
- Zero regression in escaped-defect rate: every closed chain still passes the same deterministic gates that pass today
- Legacy chain remains the default for `feature` type until A/B evidence lands (backward compatibility gate)

---

## 2. Research Insights

Verified 2026-08-13, sources on file:

- arXiv 2607.07405 ("Reason Less, Verify More"): deterministic pre-execution
  gates recovered 78% of silent wrong-state failures in tool-using agents.
  Supports daemon-owned gates; verification is our strength — consolidation
  must strengthen, not weaken.
- Workflow-vs-agent decision frameworks (Anthropic definitions; tianpan.co;
  LLM-as-Code arXiv 2606.15874): work whose path is known in advance belongs
  in deterministic workflow; only unknown-path work belongs in an agent loop.
  Drives the type→template split.
- Token economics (systemprompt.io agent-teams analysis; saihm.coti.global
  transcript-tax measurement): multi-spawn pipelines pay ~7× single-session
  tokens; transcript re-send is a quadratic tax. Per-phase spawns re-pay
  context per phase — the single-session phase-marked option attacks this.
- Temporal vs LangGraph analyses: durable execution = event-sourced state +
  replay. Hydra's park/resume + shepherd event stream is a homegrown subset;
  this PRD does NOT adopt Temporal — it notes the event-history model as the
  reference for future work.
- Live evidence (srv-smart-engine, 2026-08-12/13): RED-open/GREEN-closed
  inversions on 2 modules; ~300 machinery beads vs 58 designed; SYNTAX bead
  3a4's honest coverage holdout proved gate value; exit-0 tokens now recorded
  (PR #143) making cost measurement possible.

---

## 3. User Stories

- **US-1:** As a PRD author, I tag work as `test-coverage` and the swarm runs a
  2-phase WRITE→GATE flow instead of a 4-phase TDD chain, so tokens and
  turnaround drop without losing the coverage ratchet.
- **US-2:** As an operator, deterministic checks (lint, coverage threshold,
  diff-whitelist) run daemon-side and fail-closed, so no LLM spawn is spent on
  `npm run lint` equivalents.
- **US-3:** As a reviewer, one calibrated adversarial validator judges chain
  output, replacing five disagreeing heuristic layers.

---

## 4. Functional Requirements

### FR-001: Workflow template registry

**Type:** fr
**Priority:** P0
**Target Files:** `internal/core/workflow_template.go`, `internal/core/workflow_template_test.go`
**Test Command:** `go test ./internal/core/ -run TestWorkflowTemplate`
**Build Command:** `go build ./...`
**Reads From:** `pkg/types.Bead` (existing `Type` field)
**Depends On:** —

**Acceptance Criteria:**
- `TemplateForType(beadType string) WorkflowTemplate` returns `feature`→[RED,GREEN,REFACTOR,SYNTAX], `test-coverage`→[WRITE,GATE], `bugfix`→[REPRODUCE,FIX,GATE], `refactor`→[BASELINE-GATE,CHANGE,EQUIVALENCE-GATE]
- Unknown/empty bead type returns the legacy `feature` template (backward compatible)
- `WorkflowTemplate.Phases()` returns ordered phase list; each phase carries `RequiresLLM bool` (GATE phases are `false`)
- `go test ./internal/core/ -run TestWorkflowTemplate` exits 0

### FR-002: Daemon-side deterministic gate runner

**Type:** fr
**Priority:** P0
**Target Files:** `internal/core/gate_runner.go`, `internal/core/gate_artifact.go`, `internal/core/gate_parsers.go`, `internal/core/gate_config.go`, `internal/core/gate_runner_test.go`, `internal/core/gate_parsers_test.go`
**Test Command:** `go test ./internal/core/ -run TestGate`
**Build Command:** `go build ./...`
**Reads From:** FR-001 (`WorkflowTemplate`)
**Depends On:** FR-001

**Design (evidence-based, 2026-08-18):** session forensics on bead co3u8 showed
~85% of a verification-heavy bead's turns were the agent babysitting
deterministic commands (8 full `npm run test:cov` runs, 25M tokens for 4 file
edits). Gates therefore run daemon-side with the LLM as a *stateless consumer*
of persisted artifacts — no LLM timeout or crash can lose verification state,
because the artifact is written before any LLM involvement.

**Separation of concerns (measured against the co3u8 command log):**
- *Class A — deterministic, stack-agnostic (daemon code, zero config):* output
  capture to artifact files outside the repo, timeouts, retries, per-project
  gate lock/queue, diffstat collection, diff-whitelist enforcement.
- *Class B — deterministic, stack-specific (declared config + parser
  registry):* gate commands, thresholds, result parsers. Parsers are a small
  versioned registry: `exit-code`, `jest-coverage`, `go-test`, `pytest`.
- *Class C — judgment (LLM):* toolchain bootstrap, environment repair
  (missing deps, lockfile drift), failure diagnosis. Bootstrap produces the
  Class-B config once; the daemon verifies it by executing before freezing.

**Config schema (per project in hydra.yaml):**
```yaml
projects:
  list:
    - name: srv-smart-engine
      gates:
        daemon_owned: true            # flag, default false (legacy path)
        test:  { cmd: "npm run test:cov -- --testPathPattern=<scope>", parser: jest-coverage, threshold: 80 }
        lint:  { cmd: "npm run lint",  parser: exit-code }
        build: { cmd: "npm run build", parser: exit-code }
```

**Artifact store:** `$HYDRA_HOME/gates/<project>/<bead-id>/<gate>.json` —
atomic write (tmp+rename), `{status: running|passed|failed|interrupted,
exit_code, summary (<=2KB parsed: failing test names, coverage vs threshold),
raw_log_path, pid, started_at, finished_at}`. Raw logs live beside it as
`<gate>.log`. Never inside the project repo (kills the test-log pollution
pattern observed on srv-smart-engine).

**Resilience semantics:**
- Daemon crash mid-gate: artifact left `status: running` with PID; on restart
  the runner finalizes it if the process exited, else marks `interrupted` and
  re-runs. Gates are idempotent; re-running is always safe.
- LLM crash/timeout mid-analysis: no-op. The artifact persists; the next
  attempt (retry/park-resume) reads the same artifact.
- Close path contains zero LLM calls: pass/fail = parser output vs threshold,
  read from the artifact. Agent-quoted output is no longer evidence.
- Per-project gate lock: two `npm test` in one worktree collide, so gate runs
  serialize per project via a lock + queue; queue depth is logged as a
  fleet-health metric. Gates never hold worker slots.

**Prompt integration:** fix/validation beads receive the parsed artifact
summary (~2KB cap) + raw log path — not license to re-run the suite. Worker
prompt doctrine (TURN ECONOMY) reinforces: narrowest failing test until
green, full proof runs belong to the daemon.

**Bootstrap contract (initially manual):** gate config is hand-authored per
project and verified by a dry-run command (`hydra gates verify <project>`
executes each gate once and reports parse status) before `daemon_owned` is
flipped on. Fail-closed: no verified gates, no autonomous runs. An optional
later FR automates bootstrap as a `GATE-SETUP:` bead type (LLM proposes
config → daemon verifies → freeze); env-class gate failures escalate to
re-bootstrap instead of silent degradation.

**Acceptance Criteria:**
- `RunGate(projectPath, worktreePath string, gate GateSpec) GateResult` executes a deterministic command (lint/test/coverage) in the given worktree with a configurable timeout (default 10 minutes)
- GATE phases execute daemon-side; no worker is spawned for them (verified by zero registry rows for GATE-phase bead IDs in tests using the mock registry)
- Gate failure is fail-closed: bead stays open, the parsed artifact summary + raw log path are attached to the bead via the beads client
- Gate commands never run in the main checkout (worktree path only; main-checkout path returns error matching `/main checkout/`)
- Artifact written atomically at `$HYDRA_HOME/gates/...` before any LLM involvement; crash-recovery finalizes or re-runs `running` artifacts on daemon start
- Per-project gate lock serializes runs; concurrent RunGate calls for one project queue
- Parser registry: `exit-code`, `jest-coverage` (extracts coverage % + failing suite names), with unknown parser name returning a fail-closed error
- `hydra gates verify <project>` dry-runs configured gates and reports parse status
- `go test ./internal/core/ -run TestGate` exits 0

### FR-003: Diff-whitelist gate for test-only templates

**Type:** fr
**Priority:** P1
**Target Files:** `internal/core/gate_diff_whitelist.go`, `internal/core/gate_diff_whitelist_test.go`
**Test Command:** `go test ./internal/core/ -run TestDiffWhitelist`
**Build Command:** `go build ./...`
**Reads From:** FR-002 (`RunGate`)
**Depends On:** FR-002

**Acceptance Criteria:**
- For `test-coverage` template chains, the close gate runs `git diff --name-only <base>...HEAD` in the worktree
- Any changed file NOT matching `*.spec.ts|*.test.ts|test/**|jest.config*|docs/untestable-code-log.md` fails the gate with output listing offending paths
- Production-file diffs leave the bead open with a fail-closed error containing the literal string `production file modified in test-coverage chain`
- `go test ./internal/core/ -run TestDiffWhitelist` exits 0

### FR-004: Conditional REFACTOR phase

**Type:** fr
**Priority:** P1
**Target Files:** `internal/core/chain_executor.go`, `internal/core/chain_executor_test.go`
**Test Command:** `go test ./internal/core/ -run TestChainExecutor`
**Build Command:** `go build ./...`
**Reads From:** FR-001
**Depends On:** FR-001

**Acceptance Criteria:**
- When GREEN closes with a diff under the configured threshold (`workflow.refactor_skip_lines`, default 200 changed lines) AND `go vet`/equivalent static check passes daemon-side, the REFACTOR phase is marked `skipped` rather than spawned
- Skipped REFACTOR emits a shepherd event `refactor.skipped` with the diff stat
- Threshold `0` disables skipping (legacy behavior)
- `go test ./internal/core/ -run TestChainExecutor` exits 0

### FR-005: Single-session phase-marked execution (opt-in)

**Type:** fr
**Priority:** P2
**Target Files:** `cmd/worker/phase_runner.go`, `cmd/worker/phase_runner_test.go`
**Test Command:** `go test ./cmd/worker/ -run TestPhaseRunner`
**Build Command:** `go build ./...`
**Reads From:** FR-001, FR-002
**Depends On:** FR-002

**Acceptance Criteria:**
- When project config sets `workflow.single_session: true`, one worker process executes all LLM phases of a chain in one pi session, emitting `HYDRA_PHASE <name>` markers between phases
- Daemon-side gates still execute between markers (fail-closed aborts the session, bead annotated)
- Phase markers are parseable from the session JSONL (test replays a fixture transcript and extracts the phase sequence)
- Default remains per-phase spawns (`single_session: false`)
- `go test ./cmd/worker/ -run TestPhaseRunner` exits 0

### FR-006: Verification layer consolidation

**Type:** fr
**Priority:** P1
**Target Files:** `internal/core/validation_service.go`, `internal/core/validation_decision.go`, `internal/core/validation_decision_test.go`
**Test Command:** `go test ./internal/core/ -run TestValidationDecision`
**Build Command:** `go build ./...`
**Reads From:** —
**Depends On:** FR-001

**Acceptance Criteria:**
- A property→owner matrix is declared in code (`validation_decision.go`): each verified property (tests-pass, coverage, diff-scope, lint, AC-match, chain-completeness) has exactly one owning layer
- TDDChainPasses, RunTDDValidation, RunPRDValidation consult the matrix; properties owned by daemon gates are skipped by LLM validators
- The single adversarial validator runs once per chain at close; its verdict is logged with gap severities (telemetry for calibration)
- Existing behavior preservation: with `workflow.consolidated_validation: false` (default), all five legacy layers run unchanged
- `go test ./internal/core/ -run TestValidationDecision` exits 0

---

## 5. Non-Functional Requirements

### NFR-001: A/B measurement harness

**Type:** nfr-verification
**Verifies:** FR-001, FR-002, FR-003, FR-004, FR-005, FR-006
**Target Files:** `scripts/workflow-ab-report.sh`
**Acceptance Criteria:**
- Script queries the registry and emits per-template: beads closed, mean/p90 tokens, mean/p90 wall-clock, machinery-bead count, gate failure count
- Runs read-only against `/opt/dev-tools/var/hydra/.hydra/registry.db`
- Output includes the legacy-template cohort as control

### NFR-002: Backward compatibility gate

**Type:** nfr-verification
**Verifies:** FR-001, FR-004, FR-005, FR-006
**Acceptance Criteria:**
- All new behavior is behind config flags defaulting to legacy (`workflow.templates_enabled: false` gates FR-001..006 activation)
- `go test ./...` exits 0 with flags both true and false (CI matrix or documented manual run)

---

## 6. Implementation Map

```
FR-001 (template registry) ──► FR-002 (gate runner) ──► FR-003 (diff whitelist)
     │                           │
     │                           └──► FR-005 (single-session, P2)
     ├──► FR-004 (conditional refactor)
     └──► FR-006 (validation consolidation)
NFR-001, NFR-002 verify all (run last)
```

P0 first (FR-001, FR-002), P1 next (FR-003, FR-004, FR-006), P2 last (FR-005).
NFRs run after all FRs complete.

---

## 7. Out of Scope

- Temporal or any external durable-execution engine adoption
- Changes to park/resume mechanics
- The smart-engine in-flight run (it completes under the legacy workflow as the
  A/B control group)
- Hindsight/recall changes (owned by `memory-recall-quality-20260813` PRD)
