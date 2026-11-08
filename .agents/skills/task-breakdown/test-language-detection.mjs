/**
 * Regression tests: repo-language detection in task-breakdown (runner.mjs).
 *
 * Bug: task-breakdown had no repo-language detection. A Go repo (go.mod)
 * received JS-stack boilerplate: 'node, pnpm in PATH' prerequisites,
 * toBeCloseTo() float matcher, Biome lint gate, npm/monorepo config injection.
 *
 * Contract:
 *   - go.mod          → language 'go'      → go test/go vet criteria, no JS strings
 *   - package.json    → language 'javascript' → legacy JS boilerplate preserved
 *   - requirements.txt / pyproject.toml → 'python' → pytest wording, no JS strings
 *   - no markers      → 'unknown' → legacy behavior (conservative)
 *   - Go PRD end-to-end → zero JS-stack strings in any generated bead.
 *
 * Run: node .agents/skills/task-breakdown/test-language-detection.mjs
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { TaskBreakdownRunner } from './runner.mjs';
import { analyzePRDDeterministic } from './analyzer.mjs';

function makeRunner() {
  const runner = new TaskBreakdownRunner({ sessionId: 'test-language-detection' });
  return runner;
}

async function tempRepo(files) {
  const dir = await mkdtemp(join(tmpdir(), 'tb-lang-'));
  for (const [name, content] of Object.entries(files)) {
    await writeFile(join(dir, name), content ?? '');
  }
  return dir;
}

const JS_STRINGS = [
  'pnpm',
  'toBeCloseTo',
  'biome format',
  'npm install',
  'vitest',
  'package.json has workspaces',
];

function assertNoJsStrings(where, text, extraAllowed = []) {
  for (const needle of JS_STRINGS) {
    if (extraAllowed.includes(needle)) continue;
    assert.ok(
      !text.includes(needle),
      `${where} must not contain JS-stack string "${needle}" for a Go repo`,
    );
  }
}

// ---------------------------------------------------------------------------
// 1. Marker detection
// ---------------------------------------------------------------------------
test('detectRepoLanguage: go.mod → go', async () => {
  const runner = makeRunner();
  const dir = await tempRepo({ 'go.mod': 'module example.com/x\n\ngo 1.22\n' });
  try {
    assert.equal(await runner.detectRepoLanguage(dir), 'go');
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('detectRepoLanguage: package.json → javascript; requirements.txt/pyproject.toml → python; none → unknown', async () => {
  const runner = makeRunner();
  const js = await tempRepo({ 'package.json': '{}' });
  const py1 = await tempRepo({ 'requirements.txt': 'flask\n' });
  const py2 = await tempRepo({ 'pyproject.toml': '[project]\n' });
  const none = await tempRepo({});
  try {
    assert.equal(await runner.detectRepoLanguage(js), 'javascript');
    assert.equal(await runner.detectRepoLanguage(py1), 'python');
    assert.equal(await runner.detectRepoLanguage(py2), 'python');
    assert.equal(await runner.detectRepoLanguage(none), 'unknown');
  } finally {
    for (const d of [js, py1, py2, none]) await rm(d, { recursive: true, force: true });
  }
});

test('ensureRepoLanguage caches onto the runner instance', async () => {
  const runner = makeRunner();
  assert.equal(runner.repoLanguage, 'unknown', 'default before detection');
  const dir = await tempRepo({ 'go.mod': 'module x\n' });
  try {
    await runner.ensureRepoLanguage(dir);
    assert.equal(runner.repoLanguage, 'go');
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

// ---------------------------------------------------------------------------
// 2. Language-aware generated text (go)
// ---------------------------------------------------------------------------
test('getDevelopmentConstraints: go variant carries go vet/prereqs, no toBeCloseTo/pnpm', async () => {
  const runner = makeRunner();
  runner.repoLanguage = 'go';
  const text = runner.getDevelopmentConstraints();
  assert.ok(text.includes('go vet'), 'go constraints must mention go vet');
  assert.ok(!text.includes('toBeCloseTo'), 'go constraints must not mention toBeCloseTo');
  assert.ok(!text.includes('pnpm'), 'go constraints must not require pnpm');
  assert.ok(!text.includes('node,'), 'go constraints must not require node');
});

test('getDevelopmentConstraints: javascript/unknown keep legacy JS constraints', () => {
  for (const lang of ['javascript', 'unknown']) {
    const runner = makeRunner();
    runner.repoLanguage = lang;
    const text = runner.getDevelopmentConstraints();
    assert.ok(text.includes('toBeCloseTo'), `${lang} keeps toBeCloseTo guidance`);
    assert.ok(text.includes('pnpm'), `${lang} keeps pnpm prerequisite`);
  }
});

test('createLintGate: go variant emits go vet gate, no Biome wording', async () => {
  const runner = makeRunner();
  runner.repoLanguage = 'go';
  const created = [];
  runner.executor = {
    createTask(task, blockers) {
      created.push({ task, blockers });
      return `BRD-${created.length}`;
    },
    getReadyTasks: () => [],
  };
  runner.state = {
    async update() {},
    async load() {
      return { idMapping: {} };
    },
  };
  await runner.createLintGate({
    idMapping: { 'a-syntax': 'R1', 'b-syntax': 'R2' },
    epicIds: [],
  });
  const gate = created[0]?.task;
  assert.ok(gate, 'lint gate must be created');
  assert.ok(gate.description.includes('go vet'), 'go lint gate must run go vet');
  assert.ok(!/biome/i.test(gate.description), 'go lint gate must not mention Biome');
});

// ---------------------------------------------------------------------------
// 3. Greenfield monorepo guard: Go repo must NOT get the JS scaffolding task
// ---------------------------------------------------------------------------
const GO_PRD = `# Feature: Token Bucket Rate Limiter

### FR-001: Token bucket limiter
**Type:** fr
**Priority:** P1
**Target Files:** \`ratelimit/limiter.go\`

## Acceptance Criteria

- Tokens refill at the configured rate
- Bursts above capacity are rejected
`;

test('createTasksFromAnalysis: Go repo (go.mod) emits NO monorepo/JS config task', async () => {
  const runner = makeRunner();
  const dir = await tempRepo({ 'go.mod': 'module example.com/x\n' });
  const prevCwd = process.cwd();
  process.chdir(dir);
  try {
    const analysis = analyzePRDDeterministic(GO_PRD);
    const state = {
      prdContent: GO_PRD,
      parsedPRD: analysis,
      analysis,
      idMapping: { approvalGate: 'GATE-1' },
      reqToPhase: {},
      epicIds: [],
      currentStep: 'create-tasks',
    };
    runner.state = {
      async update() {},
      async load() {
        return state;
      },
    };
    const created = [];
    runner.executor = {
      createTask(task, blockers) {
        created.push({ task, blockers });
        return `BRD-${created.length}`;
      },
      getReadyTasks: () => [],
    };
    await runner.ensureRepoLanguage(dir);
    await runner.createTasksFromAnalysis(state);
    const all = created.map(c => c.task);
    const configTasks = all.filter(t => /Monorepo Configuration|tsconfig/i.test(t.title + t.description));
    assert.equal(configTasks.length, 0, 'Go repo must not receive JS monorepo scaffolding task');
    for (const t of all) {
      assertNoJsStrings(`bead ${t.localId} (${t.title})`, (t.description || '') + JSON.stringify(t.metadata || {}));
    }
  } finally {
    process.chdir(prevCwd);
    await rm(dir, { recursive: true, force: true });
  }
});

test('createTasksFromAnalysis: JS repo without package.json still gets monorepo config task (legacy)', async () => {
  const runner = makeRunner();
  const dir = await tempRepo({});
  const prevCwd = process.cwd();
  process.chdir(dir);
  try {
    const analysis = analyzePRDDeterministic(GO_PRD);
    const state = {
      prdContent: GO_PRD,
      parsedPRD: analysis,
      analysis,
      idMapping: { approvalGate: 'GATE-1' },
      reqToPhase: {},
      epicIds: [],
      currentStep: 'create-tasks',
    };
    runner.state = {
      async update() {},
      async load() {
        return state;
      },
    };
    const created = [];
    runner.executor = {
      createTask(task, blockers) {
        created.push({ task, blockers });
        return `BRD-${created.length}`;
      },
      getReadyTasks: () => [],
    };
    await runner.createTasksFromAnalysis(state);
    const injected = created.map(c => c.task).find(t => t.title.includes('Monorepo Configuration'));
    assert.ok(injected, 'unknown-language repo keeps legacy monorepo injection');
  } finally {
    process.chdir(prevCwd);
    await rm(dir, { recursive: true, force: true });
  }
});

// ---------------------------------------------------------------------------
// 4. TDD subtask descriptions for Go parent tasks carry no JS boilerplate
// ---------------------------------------------------------------------------
test('generateTDDSubTasks: go parent emits subtasks free of JS-stack strings', () => {
  const runner = makeRunner();
  runner.repoLanguage = 'go';
  const parent = {
    localId: 'task-fr-001',
    title: 'FR-001: Token bucket limiter',
    level: 3,
    priority: 1,
    labels: ['prd-fr'],
    metadata: {
      prd_requirement_id: 'FR-001',
      files: ['ratelimit/limiter.go'],
      functions: [],
      data_models: [],
      integration_points: [],
      test_command: 'go test ./ratelimit/ -count=1',
      build_command: 'go build ./ratelimit/',
      parent_criteria: ['Tokens refill at the configured rate'],
    },
  };
  const subs = runner.generateTDDSubTasks(parent);
  assert.ok(subs.length >= 3, 'must synthesize TDD subtasks');
  for (const sub of subs) {
    assertNoJsStrings(`subtask ${sub.localId}`, sub.description || '');
  }
});
