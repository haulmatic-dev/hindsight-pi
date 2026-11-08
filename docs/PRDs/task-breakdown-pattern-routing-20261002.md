# PRD: Task-Breakdown Pattern Routing by Deliverable Shape

## 0. Scope Profile
- **System Type:** feature
- **Component:** `.agents/skills/task-breakdown` (deterministic runner) + `.agents/skills/create-prd` (producer contract)
- **Repo:** hydra (this repo)
- **Evidence base:** fms-frontend campaign 2026-09-30 → 2026-10-02 — an analysis PRD (`System Type: cli-tool`, all Target Files `docs/code-reviews/*.md`) was compiled into 7 TDD chains (labels `tdd-red`×7, `tdd-green`×7, `tdd-syntax`×7), producing ~20 ceremony beads and ~10 duplicate PRs ("SYNTAX: Check Review Report" ×6 assignees). Root cause: `classifyRequirement` (analyzer.mjs:350) routes every `FR-*` to `TDD_CHAIN` unconditionally; it never reads the PRD's own Scope Profile, Target Files shape, or Chain column.

## 1. Problem

The task compiler has one execution pattern for functional requirements: `TDD_CHAIN` (RED→GREEN→REFACTOR→SYNTAX). That pattern is correct for code features and bugfixes, and wrong for requirements whose deliverable is a document, a test suite, an investigation, a refactor, or a deletion. The compiler already ships three non-chain patterns (`verification_gate`, `aggregation_task`, `configuration_task`) but its classifier only assigns them from requirement-ID prefixes (`NFR-`, `US-`, `SETUP-`, `Doc-`, `QA-`). Analysis-shaped FRs fall through to `TDD_CHAIN`.

Design principle: **classification = deliverable shape → verification ground truth.** Every shape's acceptance is a mechanical command; ambiguity fails **closed toward `tdd_chain`** (today's behavior).

### Routing table (target state)

| Requirement shape | Signal | Pattern | Beads | Acceptance |
|---|---|---|---|---|
| Feature/code FR | targets = code files | `tdd_chain` (unchanged) | RED→GREEN→REFACTOR→SYNTAX | chain gates |
| Bugfix FR | explicit `BUGFIX-` prefix or FR on code | `tdd_chain` (unchanged) | RED = failing repro test | chain gates |
| Docs/analysis deliverable | all targets `*.md`/`docs/` | `direct_task` (**new**) | 1 | Test Command exit 0 (verifier script) |
| Test-authoring | all targets `*.spec.*`/`*.test.*`/`*_test.*`/`tests/`/`e2e/`/`__tests__/` | `direct_task` | 1 | suite green + non-vacuity |
| Refactor of existing code | explicit `REF-` prefix | `direct_task` | 1 | existing suite stays green |
| Investigation/spike | explicit `SPIKE-` prefix | `direct_task` | 1 | findings artifact exists + non-vacuous |
| Deletion/removal | explicit `DEL-` prefix | `direct_task` | 1 | suite green + absence grep |
| Audit (findings-only) | explicit `AUDIT-` prefix | `direct_task` | 1 | verifier script: citations resolve, coverage evidence, non-vacuous |
| NFR verification | existing rules | `verification_gate` (unchanged) | 1 gate | gate command |
| US aggregation | existing rules | `aggregation_task` (unchanged) | 1 | existing |
| Setup/config | existing rules | `configuration_task` (unchanged) | 1 | existing |

Precedence (highest first): explicit requirement-ID prefix → Target Files shape heuristic → Scope Profile `System Type` default → `tdd_chain`. Classification is **per requirement, never per PRD** — a mixed audit+fix PRD gets `direct_task` for the report and `tdd_chain` for each fix.

## 2. Requirements

### FR-001: Shape-aware classification in analyzer.mjs
**Type:** fr
**Priority:** P0

**Target Files:** `.agents/skills/task-breakdown/analyzer.mjs`

**Test Command:**
```
node .agents/skills/task-breakdown/test-pattern-routing.mjs
```

**Build Command:**
```
node --check .agents/skills/task-breakdown/analyzer.mjs
```

**Reads From:** PRD markdown §0 Scope Profile, `req.files` (already parsed), requirement header regex

Three coordinated changes in one file:

1. **Parse System Type:** extend `analyzePRDDeterministic` to extract `systemType` from §0. First whitespace-delimited token after the `**System Type:**` marker is the enum value (`feature|cli-tool|analysis|docs|test-authoring`); trailing prose ignored. Absent section → `systemType: null`. Exposed on the analysis object.
2. **New prefixes:** extend the requirement-header regex with `TEST-?\d+`, `REF-?\d+`, `SPIKE-?\d+`, `AUDIT-?\d+`, `DEL-?\d+` mapping to types `Test-Authoring`, `Refactor`, `Spike`, `Audit`, `Deletion`.
3. **Classification:** add `TaskPattern.DIRECT_TASK = 'direct_task'`. In `classifyRequirement` apply precedence: (a) the five new types always classify `DIRECT_TASK` (explicit prefix wins, even with code targets); (b) `req.files` non-empty and ALL entries match docs patterns (`\.md$` or `^docs/`) → `DIRECT_TASK`; (c) ALL entries match test patterns (`\.spec\.`, `\.test\.`, `_test\.`, `^tests/`, `^e2e/`, `__tests__/`) → `DIRECT_TASK`; (d) `systemType` ∈ {`cli-tool`,`analysis`,`docs`} with no prefix/file signal → `DIRECT_TASK`; (e) otherwise existing behavior unchanged. `DIRECT_TASK` gets `needsWorktree = true`, `dependsOn = ['approvalGate']`, `dependencyType = DependencyType.BLOCKS`. Mixed code+docs targets keep `TDD_CHAIN`. Unknown/absent signals → `TDD_CHAIN` (fail-closed).

**Acceptance Criteria:**
- `analyzePRDDeterministic` returns `systemType: 'cli-tool'` for a PRD with `- **System Type:** cli-tool (analysis/documentation deliverable)`, and `null` when §0 absent
- FR with all-markdown targets classifies `direct_task` with `needsWorktree=true`
- FR with all test-file targets classifies `direct_task`
- FR with mixed code+markdown targets classifies `tdd_chain`
- Headers `TEST-001`, `REF-1`, `SPIKE-2`, `AUDIT-1`, `DEL-3` parse and classify `direct_task` regardless of targets
- Existing prefixes (`FR`, `NFR`, `US`, `US-INT`, `SETUP`, `Doc`, `QA`) parse identically to before

---

### FR-002: Single-bead synthesis for DIRECT_TASK in runner.mjs
**Type:** fr
**Priority:** P0

**Target Files:** `.agents/skills/task-breakdown/runner.mjs`

**Test Command:**
```
node .agents/skills/task-breakdown/test-pattern-routing.mjs
```

**Build Command:**
```
node --check .agents/skills/task-breakdown/runner.mjs
```

**Reads From:** `generateTDDSubTasks`, single-bead emission path used by `configuration_task`

**Depends On:** FR-001

Add `'direct_task'` to `validPatterns`. Emit exactly ONE bead per `DIRECT_TASK` requirement (title = requirement title, no `RED:`/`GREEN:` synthesis). Bead description carries the requirement's Test Command verbatim plus shape-specific acceptance lines: test-authoring → "suite exits 0; assertion count > 0; zero skipped/todo tests"; refactor → "existing suite exits 0 unchanged"; deletion → "suite exits 0; absence grep returns no matches"; spike/audit → "findings artifact exists, non-vacuous, citations resolve where applicable"; docs → "Test Command exits 0". Wire the parsed Test Command into bead metadata `test_command` so close-time gates execute it.

**Acceptance Criteria:**
- A `direct_task` requirement yields exactly one bead titled with the requirement title (no phase prefixes)
- Bead metadata contains `test_command` copied from the PRD requirement
- Bead description contains the shape-appropriate acceptance line
- Schema validation accepts `direct_task`

---

### FR-003: create-prd producer contract documentation
**Type:** fr
**Priority:** P1

**Target Files:** `.agents/skills/create-prd/SKILL.md`

**Test Command:**
```
node .agents/skills/task-breakdown/test-pattern-routing.mjs
```

**Build Command:**
```
node --check .agents/skills/task-breakdown/analyzer.mjs
```

**Reads From:** create-prd §3.1 compiler contract

Document the machine-readable contract: §0 `**System Type:**` enum (`feature|cli-tool|analysis|docs|test-authoring`, first token authoritative); requirement-ID prefix table including `TEST-`/`REF-`/`SPIKE-`/`AUDIT-`/`DEL-`; guidance that audit PRDs must supply a verifier script as Test Command (citations resolve + coverage evidence + non-vacuity) and that "no findings" is only valid with enumerated coverage evidence; the precedence rules so PRD authors can predict compilation.

**Acceptance Criteria:**
- SKILL.md contains the System Type enum with all five values
- SKILL.md documents all five new prefixes
- SKILL.md states the precedence order (prefix > target-file shape > System Type > tdd_chain)

---

### FR-004: Fixture suite proving all shapes
**Type:** fr
**Priority:** P0

**Target Files:** `.agents/skills/task-breakdown/test-pattern-routing.mjs`, `.agents/skills/task-breakdown/fixtures/fixture-analysis.md`, `.agents/skills/task-breakdown/fixtures/fixture-test-authoring.md`, `.agents/skills/task-breakdown/fixtures/fixture-refactor.md`, `.agents/skills/task-breakdown/fixtures/fixture-spike.md`, `.agents/skills/task-breakdown/fixtures/fixture-deletion.md`, `.agents/skills/task-breakdown/fixtures/fixture-audit-mixed.md`, `.agents/skills/task-breakdown/fixtures/fixture-legacy-no-scope.md`, `.agents/skills/task-breakdown/fixtures/fixture-normal-code.md`

**Test Command:**
```
node .agents/skills/task-breakdown/test-pattern-routing.mjs && node .agents/skills/task-breakdown/test-preflight.mjs && node .agents/skills/task-breakdown/test-crosscutting.mjs
```

**Build Command:**
```
node --check .agents/skills/task-breakdown/test-pattern-routing.mjs
```

**Reads From:** analyzer.mjs, runner.mjs

**Depends On:** FR-001, FR-002

Test script + 8 fixture PRDs asserting classification end-to-end: (1) analysis (System Type cli-tool, docs targets) → all FRs `DIRECT_TASK`, zero `TDD_CHAIN`; (2) test-authoring → `DIRECT_TASK`; (3) `REF-` prefix → `DIRECT_TASK`; (4) `SPIKE-` → `DIRECT_TASK`; (5) `DEL-` → `DIRECT_TASK`; (6) audit-mixed (`AUDIT-` + code FRs) → audit `DIRECT_TASK`, fixes `TDD_CHAIN`; (7) no §0 → `TDD_CHAIN` everywhere (regression guard); (8) normal code PRD → RED/GREEN/REFACTOR/SYNTAX subtask titles present, chain unchanged. Exit 0 only when all pass; print per-fixture pattern table.

**Acceptance Criteria:**
- All 8 fixtures produce their asserted pattern assignments
- Exit 1 with failing fixture names printed when any assertion fails
- `test-preflight.mjs` and `test-crosscutting.mjs` pass unmodified

---

### NFR-VALID: Existing suite stays green
**Type:** nfr-verification
**Priority:** P0

**Test Command:**
```
node .agents/skills/task-breakdown/test-preflight.mjs && node .agents/skills/task-breakdown/test-crosscutting.mjs
```

**Depends On:** FR-001, FR-002, FR-003, FR-004

**Acceptance Criteria:**
- Both pre-existing test scripts exit 0 after all changes land

## 3. Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|---|---|---|---|---|
| FR-001 | analyzer.mjs | fr (P0) | — | — |
| FR-002 | runner.mjs | fr (P0) | FR-001 | — |
| FR-003 | create-prd/SKILL.md | fr (P1) | — | — |
| FR-004 | test-pattern-routing.mjs + fixtures/ | fr (P0) | FR-001, FR-002 | — |
| NFR-VALID | — | nfr-verification | All FRs | — |

All four FRs have disjoint Target Files → FR-001, FR-003 fully parallel; FR-002 depends on FR-001 (classifier contract); FR-004 integrates. The single-file overlap rule made the analyzer change one FR — sub-steps live inside FR-001.

## 4. Edge Cases

- PRD with no §0 Scope Profile → `systemType: null` → all FRs `TDD_CHAIN` (current behavior, regression-guarded by fixture 7).
- FR with mixed code+docs targets → `TDD_CHAIN` (a docs companion file does not downgrade a code FR).
- `TEST-` prefix with code targets (malformed PRD) → explicit prefix wins; create-prd guidance tells authors not to do this.
- Empty Target Files + no prefix + System Type `analysis` → `DIRECT_TASK` via System Type default.
- Empty Target Files + no prefix + no §0 → `TDD_CHAIN` (fail-closed).
- Existing boards/beads untouched: classification applies at breakdown time only; no migration.

## 5. Non-Goals

- No changes to `verification_gate` semantics (stays worktree-less pure gate for NFRs).
- No changes to fleet close-time gates, Pan, or scheduler.
- No retroactive fix of fms beads (handled by separate fms closure beads).
