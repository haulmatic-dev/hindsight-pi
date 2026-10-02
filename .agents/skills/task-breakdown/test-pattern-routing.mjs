#!/usr/bin/env node

/**
 * Pattern-routing tests for the task-breakdown fixture suite.
 *
 * Parent acceptance criteria pinned here (RED phase — routeTaskPattern and
 * TaskRoute are NOT yet implemented; these tests must fail for the missing
 * routing feature, not for syntax errors):
 *
 * 1. All 8 fixtures produce their asserted pattern assignments
 * 2. Exit 1 with the failing fixture names printed when any assertion fails
 *
 * Reuses analyzePRDDeterministic's requirement model — no second parser.
 */

import * as fs from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

let analyzer;
let routeTaskPattern;
let TaskRoute;

try {
  analyzer = await import('./analyzer.mjs');
  routeTaskPattern = analyzer.routeTaskPattern;
  TaskRoute = analyzer.TaskRoute;
} catch (err) {
  console.error(`FAIL: cannot import analyzer.mjs routing API: ${err.message}`);
  console.error('Failing fixtures: (all — routing API unavailable)');
  process.exit(1);
}

const fixturesDir = path.join(__dirname, 'fixtures');

// Fixture name -> asserted route value. Every fixture file in the fixtures
// directory must appear in this table, and vice versa.
const expectations = {
  'fixture-analysis.md': 'analysis',
  'fixture-test-authoring.md': 'test_authoring',
  'fixture-refactor.md': 'refactor',
  'fixture-spike.md': 'spike',
  'fixture-deletion.md': 'deletion',
  'fixture-audit-mixed.md': 'audit',
  'fixture-legacy-no-scope.md': 'legacy_no_scope',
  'fixture-normal-code.md': 'normal_code',
};

const failingFixtures = [];
let passed = 0;

for (const [fixture, expected] of Object.entries(expectations)) {
  const fixturePath = path.join(fixturesDir, fixture);
  if (!fs.existsSync(fixturePath)) {
    console.error(`FAIL: ${fixture} — fixture file missing`);
    failingFixtures.push(fixture);
    continue;
  }
  const content = fs.readFileSync(fixturePath, 'utf8');
  try {
    const result = routeTaskPattern(content);
    if (!result || typeof result !== 'object') {
      console.error(`FAIL: ${fixture} — routeTaskPattern returned ${JSON.stringify(result)}, expected {route, reason}`);
      failingFixtures.push(fixture);
      continue;
    }
    if (result.route !== expected) {
      console.error(
        `FAIL: ${fixture} — asserted route ${JSON.stringify(expected)} but got ${JSON.stringify(result.route)}` +
          (result.reason ? ` (reason: ${result.reason})` : ''),
      );
      failingFixtures.push(fixture);
      continue;
    }
    passed += 1;
    console.log(`PASS: ${fixture} routes to ${expected}`);
  } catch (err) {
    console.error(`FAIL: ${fixture} — routing threw: ${err.message}`);
    failingFixtures.push(fixture);
  }
}

// Enum shape: every route value used in the table must be a member of
// TaskRoute, and TaskRoute must cover exactly the 8 shapes.
const enumValues = Object.values(TaskRoute || {});
const tableValues = Object.values(expectations);
const missingFromEnum = tableValues.filter((v) => !enumValues.includes(v));
if (enumValues.length !== 8) {
  console.error(`FAIL: TaskRoute should define exactly 8 routes, has ${enumValues.length}: ${JSON.stringify(enumValues)}`);
  failingFixtures.push('(TaskRoute enum shape)');
} else if (missingFromEnum.length > 0) {
  console.error(`FAIL: routes not covered by TaskRoute enum: ${missingFromEnum.join(', ')}`);
  failingFixtures.push('(TaskRoute enum coverage)');
}

// No stray fixture files without asserted expectations.
const onDisk = fs.existsSync(fixturesDir)
  ? fs.readdirSync(fixturesDir).filter((f) => f.startsWith('fixture-') && f.endsWith('.md'))
  : [];
const unasserted = onDisk.filter((f) => !(f in expectations));
if (unasserted.length > 0) {
  console.error(`FAIL: fixture files without asserted pattern assignments: ${unasserted.join(', ')}`);
  failingFixtures.push(...unasserted);
}

// Normal code must still flow through the existing analyzer requirement model
// unchanged (routing must not break test-crosscutting's contract shape).
const crosscutPRD = `# PRD: Crosscut Routing Check

### FR-001: Addition Operation
**Type:** fr
**Priority:** P1
**Target Files:** \`calc.ts\`
**Acceptance Criteria:**
- \`add(2, 3)\` returns \`5\`
`;
try {
  const analysis = analyzer.analyzePRDDeterministic(crosscutPRD);
  const fr1 = analysis.requirements.find((r) => r.originalId === 'FR-001');
  if (!fr1 || typeof fr1.taskPattern !== 'string') {
    console.error('FAIL: analyzePRDDeterministic no longer exposes taskPattern on requirements');
    failingFixtures.push('(analyzer integration intact)');
  }
} catch (err) {
  console.error(`FAIL: analyzer integration broke: ${err.message}`);
  failingFixtures.push('(analyzer integration intact)');
}

console.log(`\nPattern routing: ${passed}/${Object.keys(expectations).length} fixtures asserted`);

if (failingFixtures.length > 0) {
  console.error(`\nFailing fixtures: ${[...new Set(failingFixtures)].join(', ')}`);
  process.exit(1);
}

console.log('✅ All fixtures route to their asserted patterns');
