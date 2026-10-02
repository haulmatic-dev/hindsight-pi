# green handoff

## What changed (files + one line each)
- .agents/skills/task-breakdown/runner.mjs: `direct_task` added to `validPatterns` (schema validation) and sort order; new `getDirectTaskAcceptanceLine(req)` (shape lines: test-authoring/refactor/deletion/spike|audit/docs) and `createDirectTask(req, baseTask, _, state, redDeps)` — emits exactly ONE bead: title = requirement title verbatim (no RED/GREEN prefixes), description carries Test Command verbatim + shape acceptance line via formatTrackerDescription, metadata `test_command`/`build_command`/`task_pattern`/`acceptance_line`/`files`/`parent_criteria`, labels gain `direct-task`, blockers mapped from redDeps (approvalGate) same contract as TDD path, idMapping records parent localId → real bead id, `task:subtask:created` emitted with subtaskType 'direct'.
- Stats block: directTasks counted as 1 bead each in totalTasks (tddSubtasks + directTasks.length + verificationNFRs.length).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `node --check .agents/skills/task-breakdown/runner.mjs` — PASS (CHECK-OK).
- End-to-end harness (node:test, temp file, deleted after run): `createTasksFromAnalysis` invoked with a real `taskPattern='direct_task'` requirement + approval gate — exactly one direct_task bead emitted, title verbatim/no phase prefixes, `metadata.test_command` copied, description carries Test Command verbatim, blocked by approval gate, idMapping points at the direct bead id — 2 tests pass, 0 fail.
- `node --experimental-test-coverage --test` over that harness: runner.mjs 40.51% line / 53.10% branch overall; NEW code region (lines 2196–2300, `getDirectTaskAcceptanceLine` + `createDirectTask`) has ZERO uncovered lines/ranges — 100% of new implementation lines covered (next uncovered range 2302+ is pre-existing `createConfigurationTask`).
- `node .agents/skills/task-breakdown/test-preflight.mjs` — 1 pre-existing FAIL ("expected lint:skipped event or line when prd-lint is missing"); verified FAILING on clean HEAD via `git stash` (not introduced by this change; FR-001 strand area).
- `node .agents/skills/task-breakdown/test-crosscutting.mjs` — pre-existing failure: analyzer target-file overlap validation throws on the test's own fixture; also fails on clean HEAD.

## Open edges for the next phase
- FR-001 (analyzer.mjs direct_task classification) not yet in this worktree: without it, no requirement ever gets `taskPattern='direct_task'`, so the new runner branch is dormant until FR-001 lands. The pre-existing test-preflight/test-crosscutting failures likely belong to that strand.
- FR-004 test-pattern-routing.mjs will exercise this path end-to-end once FR-001 exists; the `test-pattern-routing.mjs` command in bead metadata is expected to fail until FR-004 lands it.

## Open edges for the next phase
- None beyond FR-001 dependency above.

## Open edges for the next phase
- None beyond FR-001 dependency above.

