# refactor handoff

## What changed (files + one line each)
- .agents/skills/task-breakdown/analyzer.mjs: shape-aware classification + refactor completed — `analyzePRDDeterministic` returns `systemType` (§0 `- **System Type:** cli-tool ...`; null when absent); header regex + `scanShapeSignals` handle TEST/REF/SPIKE/AUDIT/DEL/ANALYSIS-\d headers; `TaskPattern.DIRECT_TASK` — shape header → direct_task regardless of targets, all-markdown targets → direct_task needsWorktree=true, all-test targets → direct_task, mixed code+markdown → tdd_chain; legacy prefixes (FR/NFR/US/US-INT/SETUP/Doc/QA) unchanged; single-pass `scanShapeSignals(lines)` helper (no duplicated line scan).
- .agents/skills/task-breakdown/runner.mjs: `direct_task` in validPatterns + sort order; single-bead synthesis deduped into `_recordSubtaskEmission`, `DEFAULT_CONFIG_TARGET_FILES` single source.
- test-shape-classification.mjs (new, from uqrw chain): pins all parent criteria. test-direct-task.mjs (new): 6 tests pinning DIRECT_TASK synthesis contract.
- Shape-aware chain (hydra-uqrw: 274271d, 15833f9, 164220e, 03dfedc, 4d34571, 9b79b81) merged into this worktree (hydra/wt-hydra-6lmz, merge 30a2ac8, conflicts resolved to uqrw superset; duplicate TaskRoute/routeTaskPattern copy dropped in a8c28a4).
- CHANGELOG.md: Unreleased entries.

## Verification (commands run + pass/fail)
- `node test-shape-classification.mjs` — PASS, 16 assertions, "✅ Shape classification criteria met", rc=0.
- `node test-direct-task.mjs` — PASS, rc=0 (6 tests).
- `node test-pattern-routing.mjs` — PASS 8/8 fixtures, rc=0.
- `node test-preflight.mjs` — PASS rc=0.
- `node test-crosscutting.mjs` — PASS rc=0.
- No conflict markers in merged files (grep `<<<<<<<` = 0 ×3).
- No benchmarks exist for this functionality — perf check N/A.

## Open edges for the next phase
- systemType is parsed but currently informational only (returned in analysis, not consumed by runner classification beyond tests) — downstream consumers can wire it as needed.
- Untracked internal/prdcontract/ and modified .agents/skills/create-prd/SKILL.md in worktree are pre-existing, not touched by this bead.
