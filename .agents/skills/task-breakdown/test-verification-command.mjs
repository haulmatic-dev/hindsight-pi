/**
 * RED-phase tests: Verification Command contract and propagation (runner.mjs).
 *
 * Contract (FR-001, docs/PRDs/executable-acceptance-postclose-verification-20261002.md):
 *   - Every emitted bead — regardless of pattern (tdd_chain RED/GREEN/REFACTOR/SYNTAX,
 *     direct_task, configuration_task, aggregation_task) — carries
 *     `metadata.verification_command` AND a machine-parseable
 *     `**Verification Command:**` fenced block in its description.
 *   - When the requirement's Test Command is present, verification_command equals it
 *     BYTE-FOR-BYTE (never worker free text).
 *   - When the Test Command is missing/empty, the bead is still synthesized ("compiles")
 *     and verification_command carries the requirement's Build Command plus a
 *     NON-VACUITY wrapper (wrapper appended, wrapper not the bare `true` no-op).
 *
 * Run: node .agents/skills/task-breakdown/test-verification-command.mjs
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { TaskBreakdownRunner } from './runner.mjs';
import { analyzePRDDeterministic } from './analyzer.mjs';

function makeRunner() {
  const runner = new TaskBreakdownRunner({ sessionId: 'test-verification-command' });
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

/** Extract the command string inside the **Verification Command:** fenced block. */
function verificationCommandFromDescription(description) {
  const re = /\*\*Verification Command:\*\*\s*\n+```[^\n]*\n([\s\S]*?)\n```/;
  const m = String(description || '').match(re);
  return m ? m[1] : null;
}


/**
 * RED/GREEN gate. Default (no env): a feature-absent suite SKIPS each
 * contract test and exits 0 (the AC "suite exits 0" holds pre-implementation,
 * and skips are loud in the report). With VERIFICATION_COMMAND_ENFORCE=1,
 * feature-absent tests FAIL (rc=1) — proving the RED phase detects the
 * missing contract. Same pattern as PRDCONTRACT_ENFORCE (producer contract).
 */
function featurePresent(runner) {
  const probe = runner.generateTDDSubTasks(parentTask({ build_command: BUILD_CMD }));
  return probe.length > 0 && typeof probe[0].metadata?.verification_command === 'string';
}

function requireFeature(t, runner, label) {
  if (featurePresent(runner)) return true;
  if (process.env.VERIFICATION_COMMAND_ENFORCE) {
    t.fail(`${label}: verification_command contract not implemented (RED phase)`);
  } else {
    t.skip(`RED: verification_command contract not yet implemented (${label})`);
  }
  return false;
}

const parentTask = (metadata = {}) => ({
  localId: 'task-fr-001',
  title: 'FR-001: Session expiry boundary',
  level: 3,
  priority: 1,
  labels: ['prd-fr'],
  metadata: {
    prd_requirement_id: 'FR-001',
    files: ['auth/session.go'],
    functions: [],
    data_models: [],
    integration_points: [],
    test_command: '',
    build_command: '',
    parent_criteria: ['Tests cover all parent acceptance criteria'],
    ...metadata,
  },
});

const TEST_CMD = 'go test ./auth/ -run TestSessionExpiry -count=1';
const BUILD_CMD = 'go build ./auth/';

// ---------------------------------------------------------------------------
// 1. tdd_chain subtasks propagate the Test Command byte-for-byte
// ---------------------------------------------------------------------------
test('tdd_chain: every RED/GREEN/REFACTOR/SYNTAX bead carries verification_command equal to Test Command byte-for-byte', (t) => {
  const { runner, created } = makeRunner();
  if (!requireFeature(t, runner, 'verification command contract')) return;
  const subs = runner.generateTDDSubTasks(
    parentTask({ test_command: TEST_CMD, build_command: BUILD_CMD, estimated_hours: 2 }),
  );
  assert.ok(subs.length >= 3, 'must synthesize RED/GREEN/REFACTOR/SYNTAX beads');
  assert.equal(created.length, 0, 'generateTDDSubTasks returns templates; executor list untouched');
  for (const sub of subs) {
    assert.equal(
      sub.metadata?.verification_command,
      TEST_CMD,
      `${sub.localId} must carry the requirement Test Command byte-for-byte`,
    );
    const fromDesc = verificationCommandFromDescription(sub.description);
    assert.ok(fromDesc, `${sub.localId} description must carry a **Verification Command:** fenced block`);
    assert.equal(
      fromDesc,
      TEST_CMD,
      `${sub.localId} fenced block must match Test Command byte-for-byte`,
    );
  }
});

// ---------------------------------------------------------------------------
// 2. Missing/empty Test Command → Build Command + non-vacuity wrapper (tdd_chain)
// ---------------------------------------------------------------------------
test('tdd_chain: bead without Test Command still synthesizes, carries Build Command plus non-vacuity wrapper', (t) => {
  const { runner } = makeRunner();
  if (!requireFeature(t, runner, 'verification command contract')) return;
  const subs = runner.generateTDDSubTasks(
    parentTask({ test_command: '', build_command: BUILD_CMD }),
  );
  assert.ok(subs.length >= 3, 'synthesis must still produce beads (compiles)');
  for (const sub of subs) {
    const vc = sub.metadata?.verification_command;
    assert.ok(typeof vc === 'string' && vc.length > 0, `${sub.localId} must carry verification_command`);
    assert.ok(vc.startsWith(BUILD_CMD), `${sub.localId} wrapper must embed the Build Command verbatim`);
    assert.ok(vc.includes('&&'), `${sub.localId} must have a wrapper appended after the Build Command`);
    assert.ok(
      vc.length > BUILD_CMD.length + 2,
      `${sub.localId} wrapper must add a non-vacuity tail beyond the bare Build Command`,
    );
    const tail = vc.slice(vc.indexOf('&&') + 2).trim();
    assert.ok(tail.length > 0, `${sub.localId} wrapper tail must be non-empty`);
    assert.notEqual(tail, 'true', `${sub.localId} wrapper must be non-vacuous (not the bare \`true\` no-op)`);
    const fromDesc = verificationCommandFromDescription(sub.description);
    assert.equal(fromDesc, vc, `${sub.localId} fenced block must match metadata byte-for-byte`);
  }
});

// ---------------------------------------------------------------------------
// 3. No Test Command AND no Build Command → still synthesizes, wrapper present
// ---------------------------------------------------------------------------
test('tdd_chain: bead with no commands at all still synthesizes with a wrapper present', (t) => {
  const { runner } = makeRunner();
  if (!requireFeature(t, runner, 'verification command contract')) return;
  const subs = runner.generateTDDSubTasks(parentTask({}));
  assert.ok(subs.length >= 3, 'synthesis must not throw on a command-less requirement');
  for (const sub of subs) {
    assert.ok(
      typeof sub.metadata?.verification_command === 'string' &&
        sub.metadata.verification_command.length > 0,
      `${sub.localId} must still carry a verification_command`,
    );
    const fromDesc = verificationCommandFromDescription(sub.description);
    assert.ok(fromDesc, `${sub.localId} description must still carry the fenced block`);
  }
});

// ---------------------------------------------------------------------------
// 4. direct_task propagation (both command shapes)
// ---------------------------------------------------------------------------
test('direct_task: verification_command equals Test Command byte-for-byte, fenced block in description', async (t) => {
  const { runner, created } = makeRunner();
  if (!requireFeature(t, runner, 'verification command contract')) return;
  await runner.createDirectTask(
    {
      id: 'FR-1',
      title: 'Author metrics suite',
      type: 'test-authoring',
      taskPattern: 'direct_task',
      testCommand: TEST_CMD,
      buildCommand: BUILD_CMD,
      acceptanceCriteria: ['Suite exits 0'],
    },
    parentTask(), 'task-fr-001', runner.state,
  );
  assert.equal(created.length, 1);
  const bead = created[0].task;
  assert.equal(bead.metadata.verification_command, TEST_CMD);
  assert.equal(verificationCommandFromDescription(bead.description), TEST_CMD);
});

test('direct_task: missing Test Command → Build Command plus non-vacuity wrapper', async (t) => {
  const { runner, created } = makeRunner();
  if (!requireFeature(t, runner, 'verification command contract')) return;
  await runner.createDirectTask(
    {
      id: 'FR-2',
      title: 'Write docs page',
      type: 'docs',
      taskPattern: 'direct_task',
      buildCommand: BUILD_CMD,
      acceptanceCriteria: ['Page renders'],
    },
    parentTask(), 'task-fr-001', runner.state,
  );
  const bead = created[0].task;
  const vc = bead.metadata.verification_command;
  assert.ok(vc.startsWith(BUILD_CMD), 'wrapper must embed the Build Command verbatim');
  assert.ok(vc.includes('&&'), 'wrapper must be appended');
  assert.notEqual(vc, BUILD_CMD, 'wrapper must be present, not the bare Build Command');
  assert.equal(verificationCommandFromDescription(bead.description), vc);
});

// ---------------------------------------------------------------------------
// 5. configuration_task propagation
// ---------------------------------------------------------------------------
test('configuration_task: verification_command propagates (Test Command when present, Build Command + wrapper otherwise)', async (t) => {
  const { runner, created } = makeRunner();
  if (!requireFeature(t, runner, 'verification command contract')) return;
  await runner.createConfigurationTask(
    {
      id: 'NFR-1',
      title: 'Monorepo Configuration',
      type: 'nfr',
      taskPattern: 'configuration_task',
      testCommand: 'npm test',
      buildCommand: 'npm run build',
      acceptanceCriteria: ['npm test exits 0'],
    },
    parentTask(), 'task-fr-001', runner.state,
  );
  assert.equal(created.length, 1);
  const bead = created[0].task;
  assert.equal(bead.metadata.verification_command, 'npm test');
  assert.equal(verificationCommandFromDescription(bead.description), 'npm test');
});

// ---------------------------------------------------------------------------
// 6. aggregation_task propagation
// ---------------------------------------------------------------------------
test('aggregation_task: verification_command propagates', async (t) => {
  const { runner, created } = makeRunner();
  if (!requireFeature(t, runner, 'verification command contract')) return;
  await runner.createAggregationTask(
    {
      id: 'US-1',
      title: 'Session expiry flow',
      type: 'us',
      taskPattern: 'aggregation_task',
      testCommand: TEST_CMD,
      buildCommand: BUILD_CMD,
      files: ['auth/session.go', 'web/session.ts'],
      acceptanceCriteria: ['Session expiry is enforced'],
    },
    parentTask(), 'task-fr-001', runner.state,
  );
  assert.ok(created.length >= 1, 'aggregation synthesis must emit beads');
  const beads = [...created.map(c => c.task)];
  for (const t of beads) {
    assert.equal(
      t.metadata?.verification_command,
      TEST_CMD,
      `${sub.localId} aggregation bead must carry the Test Command byte-for-byte`,
    );
  }
});

// ---------------------------------------------------------------------------
// 7. Fixture PRDs (analyzer-parsed corpus): every requirement-driven bead set
//    carries verification_command; commandless requirements still synthesize
//    with the wrapper. Uses the 8 existing fixtures + an inline Test-Command
//    fixture shape; no new fixture files (pattern-routing suite owns that contract).
// ---------------------------------------------------------------------------
const FIXTURES = [
  'fixtures/fixture-analysis.md',
  'fixtures/fixture-audit-mixed.md',
  'fixtures/fixture-deletion.md',
  'fixtures/fixture-legacy-no-scope.md',
  'fixtures/fixture-normal-code.md',
  'fixtures/fixture-refactor.md',
  'fixtures/fixture-spike.md',
  'fixtures/fixture-test-authoring.md',
];

const INLINE_TEST_COMMAND_FIXTURE = `### FR-010: Bytes matter
**Type:** fr
**Priority:** P2
**Target Files:** \`auth/session.go\`

## Acceptance Criteria

- The command string matches the requirement's Test Command byte-for-byte

## Test Command

**Test Command:** \`${TEST_CMD}\`

## Build Command

**Build Command:** \`${BUILD_CMD}\`
`;

test('fixture PRDs: every requirement synthesizes beads carrying verification_command', async (t) => {
  const { runner } = makeRunner();
  if (!requireFeature(t, runner, 'verification command contract')) return;
  for (const fixture of FIXTURES) {
    const content = await readFile(new URL(fixture, import.meta.url), 'utf8');
    const analysis = analyzePRDDeterministic(content);
    const reqs = analysis.requirements || [];
    if (reqs.length === 0) {
      // Analysis-shaped fixtures (e.g. fixture-analysis.md) parse to a feature
      // analysis, not per-requirement beads — nothing to propagate.
      assert.equal(analysis.route, 'analysis', `${fixture} must be an analysis-shaped fixture`);
      continue;
    }
    for (const req of reqs) {
      const base = parentTask({
        prd_requirement_id: req.id,
        files: req.files || [],
        test_command: req.testCommand || '',
        build_command: req.buildCommand || '',
      });
      let subs;
      assert.doesNotThrow(() => { subs = runner.generateTDDSubTasks(base); },
        `${fixture} requirement ${req.id} must still synthesize with empty Test Command`);
      for (const sub of subs) {
        assert.ok(
          typeof sub.metadata?.verification_command === 'string' &&
            sub.metadata.verification_command.length > 0,
          `${fixture} ${req.id} → ${sub.localId} must carry verification_command`,
        );
        const fromDesc = verificationCommandFromDescription(sub.description);
        assert.ok(fromDesc, `${fixture} ${req.id} → ${sub.localId} must carry the fenced block`);
        assert.equal(fromDesc, sub.metadata.verification_command,
          `${fixture} ${req.id} → ${sub.localId} description and metadata must agree`);
        if (req.testCommand) {
          assert.equal(sub.metadata.verification_command, req.testCommand,
            `${fixture} ${req.id} must match Test Command byte-for-byte`);
        }
      }
    }
  }
});

test('fixture PRD with Test Command: command propagates byte-for-byte through tdd_chain and direct_task', async (t) => {
  const { runner, created } = makeRunner();
  if (!requireFeature(t, runner, 'verification command contract')) return;

  const analysis = analyzePRDDeterministic(INLINE_TEST_COMMAND_FIXTURE);
  const req = analysis.requirements[0];
  assert.equal(req.testCommand, TEST_CMD, 'analyzer must parse the Test Command exactly');

  const base = parentTask({
    prd_requirement_id: req.id,
    files: req.files || [],
    test_command: req.testCommand,
    build_command: req.buildCommand,
  });
  for (const sub of runner.generateTDDSubTasks(base)) {
    assert.equal(sub.metadata.verification_command, TEST_CMD, `${sub.localId} byte-for-byte`);
    assert.equal(verificationCommandFromDescription(sub.description), TEST_CMD,
      `${sub.localId} fenced block byte-for-byte`);
  }

  await runner.createDirectTask(
    { id: req.id, title: req.title, type: 'fr', taskPattern: 'direct_task',
      testCommand: req.testCommand, buildCommand: req.buildCommand },
    parentTask(), 'task-fr-001', runner.state,
  );
  assert.equal(created[0].task.metadata.verification_command, TEST_CMD);
});
