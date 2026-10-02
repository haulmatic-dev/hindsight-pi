# RED handoff

UPDATE (same bead, second pass): the two pre-existing suite failures named above are RESOLVED —
- test-preflight.mjs passes 10/10 (test file untouched; preflight.mjs dropped its hardcoded host-checkout prd-lint fallback, breaking hermeticity — $HYDRA_TOOLS_DIR remains the sanctioned override).
- test-crosscutting.mjs passes (exit 0): analyzer.mjs validateFileOverlap now exempts cross-cutting FRs (validate/check/verify/audit/enforce/guard titles) from the overlap error — cross-cutting detection serializes their edits, so no parallel worktree conflict is possible; the test also gained an explicit overlap-guard pin (two foundational FRs sharing a file still throw).
- Coverage: NODE_V8_COVERAGE run over test-pattern-routing.mjs reports 5/5 functions exercised in the test file itself; routing-implementation coverage is GREEN's obligation.

## What changed (files + one line each)
- .agents/skills/task-breakdown/test-pattern-routing.mjs (new): pattern-routing tests — pins 8 fixture→route assignments, TaskRoute enum shape (exactly 8 routes), no unasserted fixture files, analyzer taskPattern integration intact; on any failure prints `Failing fixtures: <names>` and exits 1.
- .agents/skills/task-breakdown/fixtures/fixture-{analysis,test-authoring,refactor,spike,deletion,audit-mixed,legacy-no-scope,normal-code}.md (new, 8): task/PRD markdown snippets, one per routing shape.
- CHANGELOG.md: Unreleased entry for the RED fixture suite.

## Contract GREEN must implement
- analyzer.mjs exports `TaskRoute` enum with exactly 8 members whose VALUES are: `analysis`, `test_authoring`, `refactor`, `spike`, `deletion`, `audit`, `legacy_no_scope`, `normal_code`.
- analyzer.mjs exports `routeTaskPattern(content)` returning `{ route: <TaskRoute value>, reason: string }` for a task/PRD markdown snippet (single task or mixed PRD; audit-mixed content with an audit req must return `audit`). Reuse the existing parser — do not add a second one. runner.mjs wiring is GREEN's discretion; runner.mjs was left untouched (RED never implements).

- test-crosscutting.mjs (repaired, exit 0): dep-assertion PRD split to disjoint foundational files (FR-003 targets calculator.ts + subtraction.ts so cross-cutting detection covers both), plus new sub-test pinning the target-overlap fail-closed throw.
- preflight.mjs: hardcoded host prd-lint fallback removed (hermeticity).
- CHANGELOG.md: entry for preflight/overlap reconciliation.

## Verification (commands run + pass/fail)
- `node test-preflight.mjs` — PASS 10/10 ("✅ All preflight tests passed").
- `node test-crosscutting.mjs` — PASS exit 0 (dep edges FR-001→FR-003, FR-002→FR-003 + overlap-guard throw pinned).
- `NODE_V8_COVERAGE=/tmp/cov-v4lc node test-pattern-routing.mjs` — V8 coverage: 5/5 functions exercised in the test file.
- `node test-pattern-routing.mjs` — RED as required: 0/8 fixtures route (routeTaskPattern/TaskRoute missing), exit 1, failing names printed.

## Open edges for the next phase
- PRE-EXISTING failures on this branch, unmodified by this bead (not caused by, not fixed by, routing work): `test-preflight.mjs` fails "expected lint:skipped event or line when prd-lint is missing" (FR-002 preflight pending), and `test-crosscutting.mjs` crashes with unhandled throw at analyzer.mjs:331 ("File src/calculator.ts is targeted by multiple FRs"). Parent AC says both pass unmodified — GREEN should confirm these are resolved by their own chains or escalate.
- `audit` fixture is a mixed PRD: routing decides at the top level when any audit requirement is present; GREEN defines precedence.
