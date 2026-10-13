---
name: create-prd
description: Create a deterministic Product Requirements Document (PRD) that acts as compiler-ready source code for a swarm execution planner. Use when defining new features, architecture work, workflows, APIs, refactors, integrations, or domain capabilities.
---

# Create PRD Skill

---

## Mental Model

This PRD is not documentation. It is source code for a deterministic compiler.

```
PRD → Task Compiler → Dependency Graph → Worktrees → RED/GREEN Chains
```

The compiler does not infer — it reads explicit fields. Every missing field is
ambiguity. Ambiguity is swarm entropy. Entropy is execution failure.

Design for compiler certainty.

---

## Execution Rules

- Execute all phases sequentially without asking for confirmation
- Ask clarifying questions only if ambiguity blocks deterministic generation (max 3)
- Never overwrite an existing PRD — increment version suffix instead
- All requirement IDs must be stable and deterministic

---

## Phase 1 — Requirements Extraction

Extract from user input:

1. Core feature or problem
2. User type or consumer
3. Business objective
4. Technical constraints
5. Existing system boundaries
6. Expected integrations and outputs
7. Domain ownership (if multi-domain)

Output: feature name (kebab-case), one-paragraph description, candidate domain
boundaries.

---

## Phase 2 — Adaptive Research

Do not blindly spawn all agents. Spawn only what the feature requires.

### Routing Rules

| Trigger | Spawn |
|---------|-------|
| Existing module touched | codebase-researcher |
| Prior feature patterns relevant | git-history-analyzer |
| New library or framework introduced | library-source-reader |
| Domain complexity or business rules present | domain-specialist |
| Existing business or system context required | context-researcher |
| Non-trivial architecture decision | best-practices-researcher |

### Execution

```yaml
subagent:
  concurrency: 4
  tasks: [only those matched by routing rules above]
```

Aggregate results into `researchContext` before Phase 3.

### Failure Policy

- **Partial failure:** Continue with available context. Mark gaps explicitly in
  Research Insights section.
- **Complete failure:** Abort. Cannot produce reliable compiler contract.

---

## Phase 3 — PRD Generation

---

### 3.1 Core Compiler Contract

Every requirement MUST include these fields. Missing fields invalidate graph
generation.

| Field | Purpose |
|-------|---------|
| `**Type:**` | Tells compiler which execution pattern to use |
| `**Priority:**` | Execution order: P0 infrastructure first, then P1 core, then P2 enhancement |
| `**Target Files:**` | Files this requirement writes — used for conflict detection |
| `**Test Command:**` | Exact shell command to verify this requirement (stack-agnostic, e.g. `go test ./...`, `pytest -xvs`) |
| `**Build Command:**` | Exact shell command to build/package this requirement (stack-agnostic, e.g. `go build ./...`, `npm run build`) |
| `**Reads From:**` | Interface/runtime dependencies satisfiable by stubs |
| `**Exports:**` | Typed interface this requirement exposes to others |
| `**Consumes:**` | Typed interfaces this requirement calls from others |

> `Exports` and `Consumes` enable full parallel RED chains only when the task
> compiler supports stub generation from `Exports`. If it does not, omit these
> two fields and use `Reads From` alone. `Reads From` is always required when
> a runtime dependency exists.

> Parser notes (task-breakdown/analyzer.mjs, verified by test-shape-classification.mjs):
> - `**Depends On:**` accepts BOTH list form (`- FR-001` lines below the marker)
>   and inline form (`**Depends On:** FR-001, FR-002` on the same line).
> - `**Reads From:**` entries are read-only dependencies — they are NOT write
>   targets and never trigger the file-overlap rule.
> - Never write placeholder glyphs (`—`, `none`) into `**Target Files:**` —
>   they parse as filenames. Omit the field entirely when a requirement has no
>   file targets (verification gates, aggregation stories).

#### 3.1a Task-Pattern Classification (producer contract)

The task-breakdown analyzer assigns every requirement an execution pattern
(`taskPattern`) before scheduling. Classification is deterministic and uses
four signals in strict precedence — strongest signal wins, weaker signals apply
only when no stronger one is present:

1. **Explicit Type prefix** — a requirement whose `**Type:**` field names a
   pattern directly (any of the five System Type enum values, see level 3) is
   honored verbatim.
2. **target-file shape** — the file extensions/paths in `**Target Files:**`
   infer the pattern (e.g. `.tsx` → web component work, `_test.go` → RED test
   pattern).
3. **System Type enum** — the PRD-level `**System Type:** backend-api | web-app
   | cli-tool | library | full-stack` supplies the default pattern for the
   whole document (backend vs. frontend surface rules).
4. **tdd_chain** — when no stronger signal applies, the requirement falls back
   to the standard TDD chain (RED → GREEN → REFACTOR → SYNTAX).

> Keep the four levels in this exact order: the analyzer mirrors it, and the
> pinned contract tests in `internal/prdcontract` fail on documentation drift.
> Level 1 always exists so an author can override any inference explicitly.
### 3.1a Execution-Type Producer Contract

The task compiler (task-breakdown) must classify every requirement into an
execution type. The producer (create-prd) declares enough signal for that
classification to be deterministic. This section IS the contract; a consumer
must not guess.

**System Type enum — exactly five values.** A requirement may declare its
system placement with a `**System Type:** value` line:

| Value | Meaning |
|-------|---------|
| `library` | Internal code consumed in-process (Go package, JS module) |
| `cli` | Command-line tool or binary the user invokes directly |
| `api` | Service surface exposed over a protocol (REST, gRPC, GraphQL) |
| `application` | End-user app or system composed of the above |
| `ops` | Deployment, cron, infra, or operational unit (systemd unit, CI job, config) |

No other values are valid. When the field is absent the consumer falls through
the precedence order below — omission is legal, but an *invalid* value is a
lint error (see `tools/prd-lint`).

**Chain prefixes — exactly five.** The TDD chain phases carried on
requirement/bead titles:

| Prefix | Phase | Meaning |
|--------|-------|---------|
| `RED:` | 1 | Tests only — proves the gap, fails green |
| `GREEN:` | 2 | Minimal implementation to pass the RED tests |
| `REFACTOR:` | 3 | Cleanup without behavior change |
| `SYNTAX:` | 4 | Build/lint/syntax verification closure |
| `FIX:` | repair | Retry of a failed chain phase |

**Classification precedence (strict order):**

1. **Prefix** — an explicit title prefix (`RED:`/`GREEN:`/`REFACTOR:`/`SYNTAX:`/`FIX:`)
   wins outright.
2. **Target-file shape** — no prefix, but every target file is a test file
   (`*_test.go`, `*.test.ts`, `test_*`) → test-only execution.
3. **System Type** — a valid enum value maps to its execution pattern
   (`library`/`api` → implementation chain; `ops` → configuration task; etc.).
4. **tdd_chain** — nothing else applies → full RED→GREEN→REFACTOR→SYNTAX chain.

A consumer MUST evaluate the layers in this order and stop at the first hit.
`**Type:**` values (`fr`, `us-light`, `us-heavy`, `us-integration`,
`nfr-verification`, `nfr-config`) are structural, not execution types — they
feed layer 3 alongside System Type, never override it.

---

### 3.2 Atomicity — Definition and Primary Rule

**An FR is atomic when it satisfies all three properties:**

1. **Precise I/O specification** — exact function signatures, file paths, and
   return values are stated; nothing is left for the implementing agent to infer
2. **Independently evaluable** — any two agents reading the ACs produce identical
   tests; there is no ambiguity in what "done" means
3. **Reusable building block** — the FR's interface is stable enough that User
   Stories can aggregate it without modification

The file-scope rule is a direct consequence of property 2: an FR cannot be
independently evaluated if another parallel task is simultaneously writing to
the same file.

---

**Primary atomicity criterion: disjoint file targets.**

Two requirements are parallelisable if and only if their `Target Files` sets are
disjoint.

**Splitting procedure:**

1. Draft the requirement with all its natural ACs
2. List the files it will touch in `Target Files`
3. If another parallel requirement shares any of those files → resolve via
   section 3.3 (do not leave the conflict unresolved)
4. Only split into separate FRs if the resulting parts have genuinely disjoint
   `Target Files`

**Never split on AC count alone.** The `FR-001a / FR-001b` pattern is
forbidden — if both parts touch the same file, the split produces a conflict,
not parallelism.

AC count as a smell signal only:
- 1–5 ACs on one file target: fine, keep together
- 6+ ACs: re-examine whether this is truly one responsibility — split only if
  targets diverge into different files

---

### 3.2b Lifetime-Fit Sizing (executability bound)

Disjoint file targets make FRs *parallelisable*. They say nothing about
whether **one worker can finish one FR in one lifetime**. A Hydra worker
lives ≈45–90 minutes of active time and ≈150–200 agent turns before the
timeout wall; every turn re-sends the full conversation, so tokens grow
quadratically with turn count. Measured in production (2026-09, srv-smart-engine
coverage campaign): beads scoped to a whole module (97 and 56 source files)
demanded 539 and 811 turns and burned 66M and 94M tokens respectively — killed
by the timeout wall and the token backstop without completing. Beads scoped to
≤12 files completed on first attempt.

**Sizing rule:** an FR's `Target Files` must contain **≤ 12 source files**
(or a single directory, whichever is smaller) when the work involves reading
and modifying each file (test-writing, refactors, migrations). Estimate
≈10–15 turns per source file; if the estimate exceeds ~150 turns, split.

**Slice + flip pattern** (for coverage/ratchet/migration epics):

1. **Slice FRs** — each covers ≤12 files. Acceptance criteria are
   *slice-scoped* (e.g. `jest <dir>` green), never module-wide metrics.
2. **Flip FR** — one terminal FR, `Depends On` all slices, tiny scope: flip
   the ratchet/config entry and run the module-wide gate ONCE. Module-wide
   numbers move only here.

**Exotic-environment separation:** work requiring test infrastructure the repo
has not used before (e.g. Temporal `TestWorkflowEnvironment`, a DB harness)
gets its own FR with an in-repo exemplar pointer (`grep -rl <pattern> src/`),
separate from bulk mechanical work. Do not bundle first-time infrastructure
with volume work — the infrastructure risk and the volume risk must fail
independently.

**Why slice-scoped acceptance matters:** module-wide acceptance forces the
worker to re-run the full module gate after every edit (measured: 186
test runs in one session, each dumping megabytes into context). Slice-scoped
runs are cheap; the expensive module-wide run happens once, in the flip FR.

---

### 3.3 Same-File Conflict Resolution

When two requirements must write to the same file, three resolutions exist.
Choose the first that applies.

---

**Resolution 1 — Merge (default)**

If the two FRs have related responsibilities, merge them into one FR. This is
the default. Do not create artificial separation to hit an AC count target.

---

**Resolution 2 — Chain (same file, genuinely separate concerns)**

When a file is large enough that two genuinely separate concerns both live
there, and merging would produce an unmanageably large task, chain them
sequentially with `Depends On`.

Chaining rules:

1. The FR that establishes the file's baseline structure is the **base FR** —
   it has no same-file dependency
2. All subsequent FRs writing to that file declare `Depends On: [base FR]`
3. If three or more FRs chain on the same file, form an explicit linear
   sequence: `FR-001 → FR-002 → FR-003`
4. Each subsequent FR's ACs must assume prior FRs are correctly implemented —
   do not re-test prior behaviour
5. Mark the chain position in the Implementation Map

**Example — auth module too large to merge:**

```markdown
### FR-001: User Model
**Target Files:** `src/auth/user.ts`
**Depends On:** —
**Acceptance Criteria:**
- `createUser(id, email)` returns User with id and email fields
- `createUser` with duplicate id throws ConflictError matching /already exists/

### FR-002: Password Hashing
**Target Files:** `src/auth/user.ts`
**Depends On:** FR-001
**Acceptance Criteria:**
- `setPassword(user, plaintext)` stores bcrypt hash on user.passwordHash
- `verifyPassword(user, plaintext)` returns true for correct password
- `verifyPassword(user, wrong)` returns false

### FR-003: Account Lock
**Target Files:** `src/auth/user.ts`
**Depends On:** FR-002
**Acceptance Criteria:**
- After 5 failed `verifyPassword` calls, user.locked is true
- `verifyPassword` on locked user throws AuthError matching /account locked/
```

The tradeoff is explicit: chaining sacrifices parallelism for task size
manageability. Accept this when a merged FR would be unreasonably large. Do not
use chaining as an excuse to avoid thinking about file structure.

---

**Resolution 3 — Restructure**

If two unrelated concerns are colliding on the same file, the file structure may
be wrong. Consider whether the file should be split at the module level before
writing the PRD. A PRD that forces chains everywhere is a signal the architecture
needs work.

---

### 3.4 Cross-Cutting Concerns

**Never create a separate FR for logic that modifies the same files as other FRs**
unless it qualifies for chaining under Resolution 2 above.

❌ Wrong — creates unresolved file conflict:
```markdown
### FR-001: Addition
**Target Files:** `src/calculator.ts:add()`

### FR-005: Input Validation        ← WRONG
**Target Files:** `src/calculator.ts` ← same file, no Depends On
```

✅ Correct — bake the cross-cutting behaviour into each FR's ACs:
```markdown
### FR-001: Addition
**Target Files:** `src/calculator.ts:add()`
**Acceptance Criteria:**
- `add(2, 3)` returns `5`
- `add(-5, 3)` returns `-2`
- `add("x", 3)` throws TypeError matching `/must be a number/`
```

The rule: if validation, error handling, or any cross-cutting behaviour writes to
the same file as the feature FR, it belongs inside that FR's ACs — not in a
separate requirement.

---

### 3.5 Testable Acceptance Criteria

**Every AC must be objectively verifiable by two independent agents and produce
identical tests.**

If two agents could read an AC and write different tests, rewrite it.

✅ Testable:
```
- `add(2, 3)` returns `5`
- `divide(5, 0)` throws RangeError with message matching `/Division by zero/`
- All public functions have JSDoc with @param, @returns, and @throws tags
```

❌ Subjective — reject these:
```
- Handles edge cases properly
- Provides clear error messages
- Code is clean and maintainable
```

**Regex pattern for error messages:** Always use `/pattern/` form so the compiler
can generate exact assertions. **Additionally, every error-message AC must include
the exact literal string the implementation must throw**, not just a regex. The
compiler injects this literal into the agent prompt to prevent paraphrase drift.

**Literal executable ACs for commands:** If a requirement has a `Test Command` or
`Build Command`, the AC must state the exact command and expected exit code:
```
- `go test ./...` exits 0
- `npm run build` exits 0 with no stderr matching `/error/i`
```

**The bar for a good AC set:** The tests implied by the ACs must both (a) pass
on a correct implementation and (b) fail on a plausible incorrect implementation.
If a buggy version of the code could satisfy all ACs, the ACs are underspecified.
Coverage percentage is a proxy metric — fault detection is the real criterion.

---

### 3.6 Requirement Types

| Type | Execution Pattern | Worktree | When to Use |
|------|------------------|----------|-------------|
| `fr` | RED → GREEN → REFACTOR → SYNTAX | Isolated | New functional logic with a specific file target |
| `nfr-verification` | VERIFY gate | Main | Coverage, quality, performance — runs after FRs complete |
| `nfr-config` | RED → GREEN → SYNTAX | Isolated | Build config, package.json, tsconfig |
| `us-light` | RED → GREEN → SYNTAX | Isolated | Pure integration: barrel exports, facades, wiring only |
| `us-heavy` | RED → GREEN → REFACTOR → SYNTAX | Isolated | Introduces new logic not present in any FR |
| `us-integration` | RED → GREEN → SYNTAX | Isolated | Integration verification for 3+ aggregated FRs |

> **Auto-generation rule:** For every 3 or more FRs that form a user-facing feature, generate a `us-integration` requirement. The us-integration bead aggregates all component FRs and runs integration tests against a live server.

### 3.6a Priority Levels

Priority controls execution order. The task compiler creates beads in priority
order, and all P1 beads implicitly depend on P0 SYNTAX completion.

| Priority | Meaning | Examples |
|----------|---------|----------|
| **P0** | Infrastructure / prerequisite | Monorepo config, TypeScript setup, build tooling, database schema |
| **P1** | Core feature | Functional requirements, API endpoints, UI components |
| **P2** | Enhancement / nice-to-have | Caching layer, analytics, advanced error reporting |

**Rules:**
- All P0 requirements run first. No P1 or P2 bead starts until all P0 SYNTAX beads close.
- **Hard gate for NFR-config:** Every `nfr-config` bead must have `Test Command` and
  `Build Command` ACs. The bead is not "closed" until those commands exit 0 in the
  worktree. Downstream beads are blocked until this gate passes.
- P1 requirements run after P0. No P2 bead starts until all P1 SYNTAX beads close.
- Within the same priority level, parallel execution follows normal dependency rules (`Depends On`, `Reads From`).
- If a requirement has no explicit `Priority:` field, default to P1 for FR/US and P0 for NFR-config.

**User Story classifier:**

- Does this story only expose or wire existing FR interfaces? → `us-light`
- Does this story introduce parsing, orchestration, or any new logic? → `us-heavy`

---

### 3.7 Dependency Declarations

All dependency fields are mandatory when applicable. Reference IDs exactly.

| Field | Semantics | Applies To |
|-------|-----------|------------|
| `Depends On` | Cannot start until target is fully complete | FR, US |
| `Reads From` | Can start with stub; needs interface to be stable | FR, US |
| `Aggregates` | Story exposes or combines these FRs | US only |
| `Verifies` | Gate runs only after all listed FRs complete | NFR-verification only |

**`Depends On`** — execution prerequisite:
```markdown
**Depends On:**
- FR-002: Password Hashing
```

**`Reads From`** — interface dependency, satisfiable by stub:
```markdown
**Reads From:**
- FR-004: Token Validation
```
Use when an FR needs to call another FR's interface but does not need to wait for
the full implementation. Enables parallel RED phases when the compiler supports
stub generation from `Exports`.

**`Aggregates`** — mandatory for all User Stories:
```markdown
**Aggregates:**
- FR-001: Addition Operation
- FR-002: Subtraction Operation
```

**`Verifies`** — mandatory for all `nfr-verification` requirements:
```markdown
**Verifies:**
- FR-001: Addition Operation
- FR-002: Subtraction Operation
```

**Without explicit dependency fields, the compiler cannot construct a correct
execution graph.**

---

### 3.8 Interface Contracts

> Omit `Exports` and `Consumes` if the compiler does not support stub generation.
> Use `Reads From` alone in that case.

**`Exports`** — typed interface this requirement exposes:
```markdown
**Exports:**
- createSession(userId: string): Session
- destroySession(sessionId: string): void
```

**`Consumes`** — typed interfaces this requirement calls:
```markdown
**Consumes:**
- validateToken(token: string): boolean
```

**Interface rules:**
- Must include full type signatures — untyped exports are not contracts
- The FR that declares `Exports` owns that interface; other FRs must not redefine it
- `Exports` must be stable before any FR that `Reads From` this requirement
  begins its RED phase

---

### 3.9 PRD Template

```markdown
# PRD: {Feature Name}

## 1. Overview

**Problem:** One sentence.
**Solution:** One sentence.
**Success Metrics:** Bullet list of measurable outcomes.

---

## 2. User Stories

### US-001: {Title}
**As a** {user}
**I want** {goal}
**So that** {benefit}

**Type:** us-light | us-heavy
**Priority:** P1

**Target Files:** `path/to/file.ts`

**Reads From:** (omit if none)
- FR-XXX: {Title}

**Exports:** (omit if compiler lacks stub support)
- functionName(param: Type): ReturnType

**Consumes:** (omit if compiler lacks stub support)
- functionName(param: Type): ReturnType

**Acceptance Criteria:**
- {Testable criterion}
- {Testable criterion}

**Aggregates:**
- FR-001: {Title}
- FR-002: {Title}

### US-INT-{NN}: {Title} — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: {Title}
- FR-002: {Title}
- FR-003: {Title}

**Acceptance Criteria:**
- `{integration-test-command}` exits 0 against live server
- All aggregated FR interfaces are reachable from integration test
- No mocking of internal dependencies in integration test

---

### Full-Stack Requirement Rule

If a User Story implies both **backend** (API, server, handler, controller) and **frontend** (web component, UI, page), you MUST create separate Functional Requirements for each:

```markdown
### FR-009: API Handler
**Target Files:** `apps/api/src/app.ts`, `apps/api/src/calculator/controller.ts`
**Acceptance Criteria:**
- `POST /api/calculate` accepts JSON body and returns result
- Error cases return 400 with correct message

### FR-010: Web Component
**Target Files:** `apps/web/src/components/Calculator.tsx`
**Acceptance Criteria:**
- Component renders input fields and operation selector
- Component displays calculation result

### US-001: Calculator Integration
**Aggregates:**
- FR-001 through FR-010
```

**Why:** One agent cannot reliably implement backend + frontend + wiring in a single bead. Separate FRs give each layer its own TDD chain (RED→GREEN→REFACTOR→SYNTAX). The US then wires them together.

---

## 3. Functional Requirements

### FR-001: {Title}
**Type:** fr
**Priority:** P1

**Target Files:** `path/to/file.ts:functionName()`

**Reads From:** (omit if none)
- FR-XXX: {Title}

**Exports:** (omit if compiler lacks stub support)
- functionName(param: Type): ReturnType

**Consumes:** (omit if compiler lacks stub support)
- otherFunction(param: Type): ReturnType

{One-paragraph description of what this requirement implements.}

**Acceptance Criteria:**
- `func(a, b)` returns `value`
- `func(bad, input)` throws ErrorType with message matching `/pattern/`
- {Measurable structural criterion}

**Depends On:** (omit if none)
- FR-XXX: {Title}

---

### 3.10 Test Standards

**Floating-point (FP) operations require `toBeCloseTo()` instead of `toBe()` for
all assertions.** Using `toBe()` on floating-point values is a common mistake
because IEEE 754 floating-point arithmetic is approximate.

**Rule:** Every AC that involves a floating-point operation must use
`toBeCloseTo()` with an appropriate `precision` parameter (typically `2` for most
math operations, or as required by the domain).

**Operations requiring floating-point equivalence:**

| Operation | Example AC |
|-----------|-----------|
| `sqrt` | `` `sqrt(4)` returns `2` `` |
| `log` | `` `log(Math.E)` returns `1` `` |
| `divide` | `` `divide(10, 3)` returns `3.333...` `` |
| `pow` | `` `pow(2, 3)` returns `8` `` |
| `sin` | `` `sin(0)` returns `0` `` |
| `cos` | `` `cos(0)` returns `1` `` |
| `tan` | `` `tan(0)` returns `0` `` |

**Example of correct FP AC:**
```
- `calculateArea(2)` returns `12.57` (toBeCloseTo 0.01)
```

**Example of incorrect FP AC (forbidden):**
```
- `calculateArea(2)` returns `12.57`  ← must use toBeCloseTo
```

---

## 4. Non-Functional Requirements

### NFR-001: {Title}
**Type:** nfr-verification | nfr-config
**Priority:** P0

**Target Files:** (nfr-config only — omit for nfr-verification)
- `package.json`
- `tsconfig.json`

**Test Command:** (nfr-config only)
```
npm test
```

**Build Command:** (nfr-config only)
```
npm run build
```

**Acceptance Criteria:**
- {Measurable criterion}
- `{test command}` exits 0
- `{build command}` exits 0

**Verifies:** (mandatory for nfr-verification; omit for nfr-config)
- FR-001: {Title}
- FR-002: {Title}

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Full project builds with `{build command}` and exits 0
- Full test suite runs with `{test command}` and exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- All `Target Files` from all FRs are importable/resolvable from the entry point

**Verifies:**
- All FRs and USs in this PRD

---

## 5. Technical Considerations

### Architecture
- Pattern chosen and rationale
- Alternatives considered and why rejected

### Technology
- Language, framework, testing tool, linter, versions

### Project Structure
```
src/
└── ...
tests/
└── ...
```

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `src/auth/user.ts` | fr | — | 1 of 3 |
| FR-002 | `src/auth/user.ts` | fr | FR-001 | 2 of 3 |
| FR-003 | `src/auth/user.ts` | fr | FR-002 | 3 of 3 |
| FR-004 | `src/session.ts` | fr | — | — |
| US-001 | `src/index.ts` | us-light | FR-001..FR-004 | — |
| NFR-001 | `package.json`, `tsconfig.json` | nfr-config | — | — |
| NFR-002 | `tests/` | nfr-verification | FR-001..FR-004 | — |
| NFR-SYS | — | nfr-verification | All FRs + USs | — |

### Edge Cases
- Domain edge cases with handling strategy

### Constraints
- Hard technical or business constraints

---

## 6. MoSCoW

**Must Have:** ...
**Should Have:** ...
**Could Have:** ...
**Won't Have:** ...

---

## 7. Research Insights

### Codebase Findings
{From codebase-researcher}

### Historical Patterns
{From git-history-analyzer}

### Domain Constraints
{From domain-specialist}

### Best Practices
{From best-practices-researcher}

### Library Constraints
{From library-source-reader}

### Project Context
{From context-researcher}
```

---

### 3.11 Validation Checklist

Run before saving. Every unchecked item is a compiler error.

**Structure**
- [ ] Every requirement has `**Type:**` with a valid value
- [ ] Every requirement has `**Priority:**` with P0, P1, or P2
- [ ] Every requirement has `**Target Files:**` with specific paths
- [ ] Every `nfr-config` requirement has `**Test Command:**` and `**Build Command:**`
- [ ] No `FR-XXXa` / `FR-XXXb` splits exist
- [ ] All User Stories have `**Aggregates:**` sections
- [ ] All `nfr-verification` requirements have `**Verifies:**` sections
- [ ] All `nfr-config` requirements do NOT have `**Verifies:**` sections
- [ ] A System Integration Verification bead (NFR-SYS) exists at the end of the PRD

**File Scope and Conflict Resolution**
- [ ] No two parallel FRs share a `Target Files` entry
- [ ] Every same-file conflict is resolved by: merge, explicit chain with
  `Depends On`, or documented restructure decision
- [ ] All chained same-file FRs appear in the Implementation Map with chain
  position (1 of N, 2 of N…)
- [ ] No cross-cutting concern FR exists without either: its logic merged into
  affected FRs' ACs, or an explicit `Depends On` chain justifying the separation

**Acceptance Criteria**
- [ ] Zero subjective ACs ("clear", "clean", "proper", "appropriate")
- [ ] All error message ACs use `/regex/` form **and** include the exact literal string
- [ ] All return value ACs use exact invocation: `` `func(a, b)` returns `value` ``
- [ ] All `nfr-config` requirements have `Test Command` and `Build Command` ACs
- [ ] A plausible buggy implementation would fail at least one AC per FR
- [ ] No AC permits an empty file or placeholder comment to pass
- [ ] All floating-point assertions use `toBeCloseTo()`, not `toBe()`

**Dependencies**
- [ ] No circular dependencies in any dependency chain
- [ ] Every ID in `Depends On`, `Reads From`, `Aggregates`, `Verifies` exists
  in the PRD
- [ ] All `Consumes` entries resolve to an `Exports` in another requirement
  (if compiler supports stubs)
- [ ] `Reads From` targets have stable `Exports` defined before dependent RED
  phases begin (if compiler supports stubs)

**User Story Aggregation**
- [ ] Integration requirements (us-integration) exist for every 3+ FR aggregation

---

## Phase 4 — Save

**Filename:** `docs/PRDs/{feature-name}-{YYYYMMDD}.md`
Increment suffix if exists: `-v2`, `-v3`. Never overwrite.

Create `docs/PRDs/` if it does not exist.

**Report:**

```
PRD saved: docs/PRDs/{filename}

Requirements:
  FR  (fr):                {n} — parallel TDD chains
  NFR (nfr-verification):  {n} — gates after FRs complete
  NFR (nfr-config):        {n} — parallel config tasks
  US  (us-light):          {n} — aggregation tasks
  US  (us-heavy):          {n} — TDD chains

Same-file chains:    {n chains} — {list files involved}
Parallel groups:     {n groups}
File conflicts:      0

Compiler stub support: YES | NO
  YES → Exports/Consumes active, parallel RED chains enabled
  NO  → Reads From only, RED chains sequential on interface dependencies

Next: run task-breakdown against this PRD when ready.
```
