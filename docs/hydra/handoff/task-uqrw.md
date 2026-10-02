# refactor handoff

## What changed (files + one line each)
- .agents/skills/task-breakdown/analyzer.mjs: (a) `routeTaskPattern` refactored — shape-signal scanning extracted into single-pass `scanShapeSignals(lines)`, names clarified (`requirements`/`signals`/`scopedReqs`); (b) shape-aware classification completed — `analyzePRDDeterministic` returns `systemType` (§0 `- **System Type:** cli-tool ...`; null when absent); requirement-header regex + shape detection extended for TEST/REF/SPIKE/AUDIT/DEL/ANALYSIS-\d headers; `TaskPattern.DIRECT_TASK` — FR with shape header → direct_task regardless of targets, all-markdown targets → direct_task needsWorktree=true, all-test targets → direct_task, mixed code+markdown → tdd_chain, legacy prefixes unchanged.
- .agents/skills/task-breakdown/runner.mjs: `direct_task` added to valid taskPattern list + sort order; creation branch (single bead via createConfigurationTask, deps approvalGate + explicit dependsOn SYNTAX ids + P0 APPLY).
- .agents/skills/task-breakdown/test-shape-classification.mjs (new): 20 assertions pinning all parent criteria (systemType, direct_task matrix, header routes, legacy prefix parity).
- CHANGELOG.md: Unreleased entries.

## Verification (commands run + pass/fail)
- BEFORE (GREEN state): `test-pattern-routing.mjs` 8/8 PASS, `test-crosscutting.mjs` PASS, `test-preflight.mjs` 10/10 PASS.
- AFTER: all three suites still PASS + `node test-shape-classification.mjs` → 20/20 assertions PASS (`✅ Shape classification criteria met`).
- `node --check analyzer.mjs runner.mjs` — SYNTAX-OK.
- No benchmarks exist for this functionality — performance check N/A.
- Commits verified via `git show --stat`.

## Open edges for the next phase
- systemType is parsed but currently informational only (returned in analysis, not consumed by runner) — downstream consumers can wire it as needed.
