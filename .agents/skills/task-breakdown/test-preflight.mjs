#!/usr/bin/env node

/**
 * RED-phase tests for the task-breakdown prd-lint preflight.
 *
 * These tests exercise cli.mjs as a black-box subprocess and fail when the
 * preflight behaviour described in FR-002 is not yet implemented.
 */

import { spawnSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const cliPath = path.resolve(__dirname, 'cli.mjs');

let failures = 0;

function fail(message) {
  console.error(`FAIL: ${message}`);
  failures += 1;
}

function pass(message) {
  console.log(`PASS: ${message}`);
}

function makeTempDir() {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'tb-preflight-'));
}

function makeFixturePrd(dir) {
  const prdPath = path.join(dir, 'prd.md');
  fs.writeFileSync(
    prdPath,
    `# PRD: Preflight Test

### FR-001: Dummy Requirement
**Type:** fr
**Priority:** P1
**Target Files:** \`dummy.go\`
**Acceptance Criteria:**
- Passes tests
`,
  );
  return prdPath;
}

function makePrdLintStub(dir, exitCode, stdout = '') {
  const stubDir = path.join(dir, 'prd-lint');
  fs.mkdirSync(stubDir, { recursive: true });
  const stubPath = path.join(stubDir, 'prd-lint');
  const script =
    exitCode === 2
      ? `#!/usr/bin/env bash\necho "prd-lint crashed" >&2\nexit 2\n`
      : `#!/usr/bin/env bash\n${stdout ? `printf '%s\\n' '${stdout.replace(/'/g, "'\\''")}'` : ''}\nexit ${exitCode}\n`;
  fs.writeFileSync(stubPath, script);
  fs.chmodSync(stubPath, 0o755);
  return stubPath;
}

function initBr(dir) {
  const result = spawnSync('br', ['init', '--prefix', 'preflight', '--quiet'], {
    cwd: dir,
    encoding: 'utf8',
  });
  if (result.status !== 0) {
    throw new Error(
      `br init failed in ${dir}: ${result.stderr || result.stdout || 'unknown error'}`,
    );
  }
}

function runCli(args, env, cwd) {
  const result = spawnSync(process.execPath, [cliPath, ...args], {
    cwd,
    env: { ...process.env, ...env },
    encoding: 'utf8',
    timeout: 60_000,
  });
  const lines = `${result.stdout}\n${result.stderr}`.split('\n');
  const events = [];
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      events.push(JSON.parse(trimmed));
    } catch {
      // Non-JSON lines (human logs, warnings) are ignored here.
    }
  }
  return { ...result, events, output: result.stdout + result.stderr };
}

function cleanup(dir) {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
    // Ignore cleanup failures.
  }
}

// ---------------------------------------------------------------------------
// Test 1: cli.mjs syntax is valid.
// ---------------------------------------------------------------------------
{
  const result = spawnSync(process.execPath, ['--check', cliPath], {
    encoding: 'utf8',
  });
  if (result.status === 0) {
    pass('node --check cli.mjs exits 0');
  } else {
    fail(
      `node --check cli.mjs exited ${result.status}: ${result.stderr || result.stdout}`,
    );
  }
}

// ---------------------------------------------------------------------------
// Test 2: a failing prd-lint stub blocks decomposition with PRD_LINT_FAILED.
// ---------------------------------------------------------------------------
{
  const tmp = makeTempDir();
  try {
    initBr(tmp);
    const prdPath = makeFixturePrd(tmp);
    makePrdLintStub(
      tmp,
      1,
      '{"passed":false,"violations":[{"rule":"R2-priority","id":"FR-001","line":10,"message":"missing Priority"}]}',
    );

    const result = runCli(
      [prdPath, '--json', '--session-id', 't2-fail'],
      { HYDRA_TOOLS_DIR: tmp },
      tmp,
    );

    if (result.status === 1) {
      pass('failing lint stub causes exit 1');
    } else {
      fail(`expected exit 1 with failing lint stub, got ${result.status}`);
    }

    const lintFailed = result.events.find(
      (e) => e.type === 'error' && e.code === 'PRD_LINT_FAILED',
    );
    if (lintFailed) {
      pass('PRD_LINT_FAILED error event is emitted');
    } else {
      fail('expected PRD_LINT_FAILED error event, but none found');
    }

    const taskCreated = result.events.find(
      (e) => e.type === 'task:created' || e.type === 'task:subtask:created',
    );
    if (!taskCreated) {
      pass('no task:created events are emitted when lint fails');
    } else {
      fail(`expected no task creation, found ${taskCreated.type}`);
    }
  } finally {
    cleanup(tmp);
  }
}

// ---------------------------------------------------------------------------
// Test 3: a passing prd-lint stub allows the runner to start.
// ---------------------------------------------------------------------------
{
  const tmp = makeTempDir();
  try {
    initBr(tmp);
    const prdPath = makeFixturePrd(tmp);
    makePrdLintStub(tmp, 0, '{"passed":true,"violations":[]}');

    const result = runCli(
      [prdPath, '--json', '--session-id', 't3-pass'],
      { HYDRA_TOOLS_DIR: tmp },
      tmp,
    );

    const started = result.events.find(
      (e) => e.type === 'workflow:start' || e.type === 'validation:start',
    );
    if (started) {
      pass('passing lint stub allows runner to start');
    } else {
      fail(
        'expected workflow:start or validation:start after passing lint stub',
      );
    }
  } finally {
    cleanup(tmp);
  }
}

// ---------------------------------------------------------------------------
// Test 4: when no prd-lint binary is resolvable, the preflight is skipped.
// ---------------------------------------------------------------------------
{
  const tmp = makeTempDir();
  try {
    initBr(tmp);
    const prdPath = makeFixturePrd(tmp);
    // Intentionally do not create a prd-lint binary. The CWD is the temp
    // directory, so cwd/tools/prd-lint/prd-lint does not exist.

    const result = runCli(
      [prdPath, '--json', '--session-id', 't4-skip'],
      {},
      tmp,
    );

    const skipped = result.events.find((e) => e.type === 'lint:skipped');
    const skippedLine = /lint:skipped|prd-lint not found/i.test(result.output);
    if (skipped || skippedLine) {
      pass('missing prd-lint binary is reported as skipped');
    } else {
      fail('expected lint:skipped event or line when prd-lint is missing');
    }
  } finally {
    cleanup(tmp);
  }
}

// ---------------------------------------------------------------------------
// Test 5: --no-lint bypasses the preflight even when a failing stub exists.
// ---------------------------------------------------------------------------
{
  const tmp = makeTempDir();
  try {
    initBr(tmp);
    const prdPath = makeFixturePrd(tmp);
    makePrdLintStub(
      tmp,
      1,
      '{"passed":false,"violations":[{"rule":"R2-priority","id":"FR-001","line":10,"message":"missing Priority"}]}',
    );

    const result = runCli(
      [prdPath, '--json', '--no-lint', '--session-id', 't5-nolint'],
      { HYDRA_TOOLS_DIR: tmp },
      tmp,
    );

    const lintFailed = result.events.find(
      (e) => e.type === 'error' && e.code === 'PRD_LINT_FAILED',
    );
    if (lintFailed) {
      fail('--no-lint should bypass the failing lint stub');
    } else {
      pass('--no-lint does not emit PRD_LINT_FAILED');
    }

    const started = result.events.find(
      (e) => e.type === 'workflow:start' || e.type === 'validation:start',
    );
    if (started) {
      pass('--no-lint allows runner to start');
    } else {
      fail('expected workflow:start or validation:start with --no-lint');
    }
  } finally {
    cleanup(tmp);
  }
}

// ---------------------------------------------------------------------------
// Test 6: a crashing prd-lint stub (exit 2) is treated as fail-open.
// ---------------------------------------------------------------------------
{
  const tmp = makeTempDir();
  try {
    initBr(tmp);
    const prdPath = makeFixturePrd(tmp);
    makePrdLintStub(tmp, 2);

    const result = runCli(
      [prdPath, '--json', '--session-id', 't6-crash'],
      { HYDRA_TOOLS_DIR: tmp },
      tmp,
    );

    const warning = /prd-lint/i.test(result.output);
    if (warning) {
      pass('crashing prd-lint stub emits a warning');
    } else {
      fail('expected a warning line matching /prd-lint/ when lint crashes');
    }

    const started = result.events.find(
      (e) => e.type === 'workflow:start' || e.type === 'validation:start',
    );
    if (started) {
      pass('crashing lint stub does not block decomposition');
    } else {
      fail('expected decomposition to proceed after lint crash');
    }
  } finally {
    cleanup(tmp);
  }
}

if (failures === 0) {
  console.log('\n✅ All preflight tests passed');
  process.exit(0);
} else {
  console.error(`\n❌ ${failures} preflight test(s) failed`);
  process.exit(1);
}
