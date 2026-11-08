/**
 * Tests: Single-bead synthesis for DIRECT_TASK in runner.mjs
 * Covers acceptance criteria A–D:
 *   A. `direct_task` is a recognized valid pattern in createTasksFromAnalysis routing
 *      (verified structurally here via createDirectTask contract; full routing
 *      pinned by fixture suite where applicable).
 *   B. Exactly ONE bead is emitted per direct_task requirement (no RED/GREEN/REFACTOR/SYNTAX).
 *   C. Emitted bead carries verbatim title, test_command metadata, shape acceptance
 *      line, and `direct-task` label.
 *   D. Emitted bead is blocked by the mapped real blocker ids (approval gate).
 *
 * Run: node .agents/skills/task-breakdown/test-direct-task.mjs
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { TaskBreakdownRunner } from './runner.mjs';

function makeRunner() {
  const runner = new TaskBreakdownRunner({ sessionId: 'test-direct' });
  const created = [];
  runner.executor = {
    createTask(task, blockers) {
      created.push({ task, blockers });
      return `BRD-${created.length}`;
    },
  };
  const state = {
    idMapping: { approvalGate: 'BRD-GATE' },
    async update() {},
  };
  runner.state = state;
  return { runner, created, state };
}

const parentTask = (localId = 'task-fr-001') => ({
  localId,
  title: 'parent',
  level: 3,
  priority: 1,
  labels: ['prd-fr'],
});

test('B: emits exactly one bead — no phase synthesis', async () => {
  const { runner, created } = makeRunner();
  await runner.createDirectTask(
    { id: 'FR-1', title: 'Write docs page', type: 'docs', taskPattern: 'direct_task' },
    parentTask(), 'task-fr-001', runner.state,
  );
  assert.equal(created.length, 1);
});

test('C1: title verbatim — no RED/GREEN phase prefixes', async () => {
  const { runner, created } = makeRunner();
  await runner.createDirectTask(
    { id: 'FR-2', title: 'Author metrics suite', type: 'test-authoring' },
    parentTask(), 'task-fr-001', runner.state,
  );
  assert.equal(created[0].task.title, 'Author metrics suite');
  assert.ok(!/^(RED|GREEN|REFACTOR|SYNTAX):/.test(created[0].task.title));
});

test('C2: test_command metadata copied and description carries Test Command verbatim', async () => {
  const { runner, created } = makeRunner();
  const cmd = 'node test-thing.mjs';
  await runner.createDirectTask(
    { id: 'FR-3', title: 'Author metrics suite', type: 'test-authoring', testCommand: cmd },
    parentTask(), 'task-fr-001', runner.state,
  );
  const t = created[0].task;
  assert.equal(t.metadata.test_command, cmd);
  assert.ok(t.description.includes(cmd), 'description must carry Test Command verbatim');
  assert.ok(t.metadata.task_pattern === 'direct_task');
});

test('C3: shape acceptance line — test-authoring gets assertion-contract line', async () => {
  const { runner, created } = makeRunner();
  await runner.createDirectTask(
    { id: 'FR-4', title: 'Suite', type: 'test-authoring' },
    parentTask(), 'task-fr-001', runner.state,
  );
  const t = created[0].task;
  assert.ok(t.metadata.acceptance_line.includes('assertion count > 0'));
  assert.ok(t.labels.includes('direct-task'));
});

test('C4: refactor shape acceptance line', async () => {
  const { runner, created } = makeRunner();
  await runner.createDirectTask(
    { id: 'FR-5', title: 'Tidy', type: 'refactor' },
    parentTask(), 'task-fr-001', runner.state,
  );
  assert.ok(created[0].task.metadata.acceptance_line.includes('exits 0 unchanged'));
});

test('D: blockers mapped from redDeps real ids; idMapping records parent localId', async () => {
  const { runner, created, state } = makeRunner();
  await runner.createDirectTask(
    { id: 'FR-6', title: 'Audit logs', type: 'audit' },
    parentTask(), 'task-fr-001', runner.state, ['approvalGate'],
  );
  assert.deepEqual(created[0].blockers, ['BRD-GATE']);
  assert.equal(state.idMapping['task-fr-001'], 'BRD-1');
});
