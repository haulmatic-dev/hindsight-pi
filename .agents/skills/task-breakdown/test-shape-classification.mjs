#!/usr/bin/env node

/**
 * Shape-aware classification tests: systemType parsing + direct_task routing.
 *
 * Pins:
 * - analyzePRDDeterministic returns systemType from `- **System Type:** ...`
 *   and null when the Scope Profile is absent.
 * - FR with all-markdown targets → direct_task (needsWorktree=true).
 * - FR with all test-file targets → direct_task.
 * - FR with mixed code+markdown targets → tdd_chain.
 * - Shape-bearing headers (TEST-001, REF-1, SPIKE-2, AUDIT-1, DEL-3) classify
 *   direct_task regardless of targets.
 * - Existing prefixes (FR, NFR, US, US-INT, SETUP, Doc, QA) parse identically.
 */

import { analyzePRDDeterministic, TaskPattern } from './analyzer.mjs';

const failures = [];
const check = (name, cond) => {
  if (cond) console.log(`PASS: ${name}`);
  else {
    console.error(`FAIL: ${name}`);
    failures.push(name);
  }
};

const fr = (analysis, id) => analysis.requirements.find((r) => r.originalId === id);

// --- systemType parsing ------------------------------------------------------
const sysTypePRD = `# PRD: Tool
## §0. Scope Profile
- **System Type:** cli-tool (analysis/documentation deliverable)
### FR-001: Parse input
**Target Files:** \`src/parse.ts\`
**Acceptance Criteria:**
- parses input
`;
check('systemType = cli-tool from Scope Profile', analyzePRDDeterministic(sysTypePRD).systemType === 'cli-tool');

const noSysTypePRD = `# PRD: Tool
### FR-001: Parse input
**Target Files:** \`src/parse.ts\`
**Acceptance Criteria:**
- parses input
`;
check('systemType = null when §0 absent', analyzePRDDeterministic(noSysTypePRD).systemType === null);

// --- target-shape routing ----------------------------------------------------
const allMarkdownPRD = `# PRD: Docs
### FR-001: Write guide
**Target Files:** \`docs/guide.md\`, \`docs/notes.md\`
**Acceptance Criteria:**
- guide written
`;
const allMd = analyzePRDDeterministic(allMarkdownPRD);
check('all-markdown targets → direct_task', fr(allMd, 'FR-001').taskPattern === TaskPattern.DIRECT_TASK);
check('all-markdown targets → needsWorktree=true', fr(allMd, 'FR-001').needsWorktree === true);

const allTestPRD = `# PRD: Tests
### FR-001: Author tests
**Target Files:** \`tests/calculator.test.ts\`
**Acceptance Criteria:**
- tests authored
`;
check('all test-file targets → direct_task', fr(analyzePRDDeterministic(allTestPRD), 'FR-001').taskPattern === TaskPattern.DIRECT_TASK);

const mixedPRD = `# PRD: Mixed
### FR-001: Feature work
**Target Files:** \`src/feature.ts\`, \`docs/feature.md\`
**Acceptance Criteria:**
- feature implemented
`;
check('mixed code+markdown targets → tdd_chain', fr(analyzePRDDeterministic(mixedPRD), 'FR-001').taskPattern === TaskPattern.TDD_CHAIN);

// --- shape-bearing headers, regardless of targets ------------------------------
const headerRoutes = {
  'TEST-001': 'Test runner',
  'REF-1': 'Rename helpers',
  'SPIKE-2': 'Probe cache',
  'AUDIT-1': 'Audit deps',
  'DEL-3': 'Remove dead code',
};
for (const [header, title] of Object.entries(headerRoutes)) {
  const prd = `# PRD: Headers
### ${header}: ${title}
**Target Files:** \`src/impl.ts\`
**Acceptance Criteria:**
- done
`;
  const req = fr(analyzePRDDeterministic(prd), header);
  check(`${header} header → direct_task regardless of targets`, req?.taskPattern === TaskPattern.DIRECT_TASK);
}

// --- existing prefixes parse identically ----------------------------------------
const legacyPRD = `# PRD: Legacy
### FR-001: Feature
**Acceptance Criteria:**
- works
### NFR-001: Coverage
**Acceptance Criteria:**
- 90% coverage
### US-001: Story
**Acceptance Criteria:**
- told
### US-INT-001: Integration
**Acceptance Criteria:**
- integrated
### SETUP-001: Bootstrap
**Acceptance Criteria:**
- bootstrapped
### Doc-001: Guide
**Acceptance Criteria:**
- documented
### QA-001: Verify
**Acceptance Criteria:**
- verified
`;
const legacy = analyzePRDDeterministic(legacyPRD);
const typeOf = (id) => legacy.requirements.find((r) => r.originalId === id)?.type;
check('FR parses as FR', typeOf('FR-001') === 'FR');
check('NFR parses as NFR', typeOf('NFR-001') === 'NFR');
check('US parses as User Story', typeOf('US-001') === 'User Story');
check('US-INT parses as User Story Integration', typeOf('US-INT-001') === 'User Story Integration');
check('SETUP parses as Setup', typeOf('SETUP-001') === 'Setup');
check('Doc parses as Docs', typeOf('DOC-001') === 'Docs');
check('QA parses as QA', typeOf('QA-001') === 'QA');
check('legacy FR without targets stays tdd_chain', fr(legacy, 'FR-001').taskPattern === TaskPattern.TDD_CHAIN);
check('legacy NFR stays verification_gate', fr(legacy, 'NFR-001').taskPattern === TaskPattern.VERIFICATION_GATE);

// --- System Type fallback: docs-tree deliverables in cli-tool/analysis/docs campaigns ----------
const docsTreeCliToolPRD = `# PRD: Tool
## §0. Scope Profile
- **System Type:** cli-tool (analysis/documentation deliverable)
### FR-000: Scaffolding
**Target Files:** \`docs/reviews/rubric.md\`, \`docs/reviews/verify-citations.sh\`
**Acceptance Criteria:**
- verifier exits 0
### FR-001: Report A
**Target Files:** \`docs/reviews/a.md\`
**Acceptance Criteria:**
- report written
`;
const docsTree = analyzePRDDeterministic(docsTreeCliToolPRD);
check('cli-tool PRD: docs-tree scaffolding (md + verifier .sh) → direct_task', fr(docsTree, 'FR-000').taskPattern === TaskPattern.DIRECT_TASK);
check('cli-tool PRD: no tdd_chain emitted', docsTree.requirements.every((r) => r.taskPattern !== TaskPattern.TDD_CHAIN));

const docsTreeFeaturePRD = `# PRD: Feature
## §0. Scope Profile
- **System Type:** feature
### FR-001: Feature work
**Target Files:** \`src/feature.ts\`, \`docs/feature.md\`
**Acceptance Criteria:**
- implemented
`;
check('feature PRD: System Type fallback does not fire (mixed targets stay tdd_chain)', fr(analyzePRDDeterministic(docsTreeFeaturePRD), 'FR-001').taskPattern === TaskPattern.TDD_CHAIN);

const cliToolCodePRD = `# PRD: Tool
## §0. Scope Profile
- **System Type:** cli-tool
### FR-001: Implement parser
**Target Files:** \`src/parse.ts\`
**Acceptance Criteria:**
- parses input
`;
check('cli-tool PRD: code-targeted FR outside docs tree stays tdd_chain', fr(analyzePRDDeterministic(cliToolCodePRD), 'FR-001').taskPattern === TaskPattern.TDD_CHAIN);

// --- Depends On parsing (list + inline forms) --------------------------------
const depsListPRD = `# PRD: Deps
### FR-001: Base
**Target Files:** \`src/base.ts\`
**Acceptance Criteria:**
- works
### FR-002: Dependent
**Target Files:** \`src/dep.ts\`
**Depends On:**
- FR-001
**Acceptance Criteria:**
- works too
`;
check('Depends On list form parses into explicitDependsOn', analyzePRDDeterministic(depsListPRD).requirements.find((r) => r.originalId === 'FR-002')?.explicitDependsOn?.includes('FR-001') === true);

const depsInlinePRD = `# PRD: Deps inline
### FR-001: Base
**Target Files:** \`src/base.ts\`
**Acceptance Criteria:**
- works
### FR-002: Dependent
**Target Files:** \`src/dep.ts\`
**Depends On:** FR-001, FR-003
**Acceptance Criteria:**
- works too
### FR-003: Other base
**Target Files:** \`src/other.ts\`
**Acceptance Criteria:**
- works three
`;
const inlineDeps = analyzePRDDeterministic(depsInlinePRD).requirements.find((r) => r.originalId === 'FR-002')?.explicitDependsOn || [];
check('Depends On inline form parses both IDs', inlineDeps.includes('FR-001') && inlineDeps.includes('FR-003'));

// --- Reads From is not a write target ------------------------------------------
const readsFromPRD = `# PRD: Reads
### FR-001: Owns config
**Target Files:** \`jest.config.ts\`
**Acceptance Criteria:**
- written
### FR-002: Documents config
**Target Files:** \`docs/config.md\`
**Reads From:** \`jest.config.ts\`
**Acceptance Criteria:**
- documented
`;
let readsOverlap = null;
try {
  const a = analyzePRDDeterministic(readsFromPRD);
  readsOverlap = a.requirements.find((r) => r.originalId === 'FR-002')?.readsFrom?.includes('jest.config.ts') ? null : 'readsFrom not tracked';
} catch (e) {
  readsOverlap = e.message;
}
check('Reads From file shared with another FR does not trigger overlap', readsOverlap === null);

console.log(`\nShape classification: ${18 + Object.keys(headerRoutes).length} assertions`);
if (failures.length > 0) {
  console.error(`\nFailing assertions: ${failures.join(', ')}`);
  process.exit(1);
}
console.log('✅ Shape classification criteria met');
