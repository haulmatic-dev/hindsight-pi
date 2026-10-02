# RED handoff

## What changed (files + one line each)
- .agents/skills/task-breakdown/test-pattern-routing.mjs (new): pattern-routing tests — pins 8 fixture→route assignments, TaskRoute enum shape (exactly 8 routes), no unasserted fixture files, analyzer taskPattern integration intact; on any failure prints `Failing fixtures: <names>` and exits 1.
- .agents/skills/task-breakdown/fixtures/fixture-{analysis,test-authoring,refactor,spike,deletion,audit-mixed,legacy-no-scope,normal-code}.md (new, 8): task/PRD markdown snippets, one per routing shape.
- CHANGELOG.md: Unreleased entry for the RED fixture suite.

## Contract GREEN must implement
- analyzer.mjs exports `TaskRoute` enum with exactly 8 members whose VALUES are: `analysis`, `test_authoring`, `refactor`, `spike`, `deletion`, `audit`, `legacy_no_scope`, `normal_code`.
- analyzer.mjs exports `routeTaskPattern(content)` returning `{ route: <TaskRoute value>, reason: string }` for a task/PRD markdown snippet (single task or mixed PRD; audit-mixed content with an audit req must return `audit`). Reuse the existing parser — do not add a second one. runner.mjs wiring is GREEN's discretion; runner.mjs was left untouched (RED never implements).

## Verification (commands run + pass/fail)
- `node .agents/skills/task-breakdown/test-pattern-routing.mjs` — FAIL for the RIGHT reason: every fixture reports "routing threw: routeTaskPattern is not a function", enum-shape check fails with "has 0: []", prints all 8 failing fixture names, standalone exit code 1 (verified without pipe). PASS as RED.
- Coverage: not measurable for new code in RED (only the test file exists; `routeTaskPattern` has no implementation to cover). GREEN must measure coverage on the new routing code.

## Open edges for the next phase
- PRE-EXISTING failures on this branch, unmodified by this bead (not caused by, not fixed by, routing work): `test-preflight.mjs` fails "expected lint:skipped event or line when prd-lint is missing" (FR-002 preflight pending), and `test-crosscutting.mjs` crashes with unhandled throw at analyzer.mjs:331 ("File src/calculator.ts is targeted by multiple FRs"). Parent AC says both pass unmodified — GREEN should confirm these are resolved by their own chains or escalate.
- `audit` fixture is a mixed PRD: routing decides at the top level when any audit requirement is present; GREEN defines precedence.
