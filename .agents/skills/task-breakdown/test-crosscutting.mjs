import { analyzePRDDeterministic } from './analyzer.mjs';

// Simple test PRD with cross-cutting concern
const testPRD = `# Product Requirements Document: Simple Calculator Module

## 3. Functional Requirements

### FR-001: Addition Operation
**Acceptance Criteria:**
- \`add(2, 3)\` returns \`5\`
- \`add(-5, 3)\` returns \`-2\`
- \`add(0, 5)\` returns \`5\`

### FR-002: Subtraction Operation
**Acceptance Criteria:**
- \`subtract(5, 3)\` returns \`2\`
- \`subtract(3, 5)\` returns \`-2\`
- \`subtract(0, 5)\` returns \`-5\`

### FR-003: Input Validation
**Acceptance Criteria:**
- Validate operands are of type 'number'
- Reject NaN values with TypeError
- Reject non-number types with TypeError

## 5. Technical Considerations

### 5.8 Implementation Mapping

| Requirement | Files | Integration Points | Data Models | Depends On | Verifies |
|-------------|-------|-------------------|-------------|------------|----------|
| FR-001 | \`src/calculator.ts:add()\` | Barrel export | - | - | - |
| FR-002 | \`src/calculator.ts:subtract()\` | Barrel export | - | - | - |
| FR-003 | \`src/calculator.ts\` | Barrel export | - | FR-001, FR-002 | - |
`;

console.log('Testing cross-cutting concern detection...\n');

const analysis = analyzePRDDeterministic(testPRD);

// Verify FR-003 now depends on FR-001 and FR-002
const fr003 = analysis.requirements.find(r => r.originalId === 'FR-003');
if (!fr003) {
  console.error('FAIL: FR-003 not found');
  process.exit(1);
}

if (!fr003.dependsOn?.includes('FR-001')) {
  console.error('FAIL: FR-003 should depend on FR-001');
  process.exit(1);
}

if (!fr003.dependsOn?.includes('FR-002')) {
  console.error('FAIL: FR-003 should depend on FR-002');
  process.exit(1);
}

// Verify semantic graph has the dependency edges
const hasFR001toFR003 = analysis.semanticGraph?.edges?.some(
  e => e.from === 'FR-001' && e.to === 'FR-003'
);
const hasFR002toFR003 = analysis.semanticGraph?.edges?.some(
  e => e.from === 'FR-002' && e.to === 'FR-003'
);

if (!hasFR001toFR003) {
  console.error('FAIL: Semantic graph should have FR-001 -> FR-003 edge');
  process.exit(1);
}

if (!hasFR002toFR003) {
  console.error('FAIL: Semantic graph should have FR-002 -> FR-003 edge');
  process.exit(1);
}

console.log('✅ PASS: Cross-cutting detection works correctly');
console.log(`   FR-003 depends on: ${fr003.dependsOn.join(', ')}`);
console.log(`   Semantic graph has FR-001 -> FR-003 and FR-002 -> FR-003 edges`);
