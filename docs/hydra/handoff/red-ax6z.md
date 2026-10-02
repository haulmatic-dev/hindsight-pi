# red handoff (hydra-ax6z)

## What changed (files + one line each)
- .agents/skills/task-breakdown/test-verification-command.mjs (new): 9 node:test cases pinning the FR-001 Verification Command contract — `metadata.verification_command` + machine-parseable `**Verification Command:**` fenced description block on every emitted bead across all four synthesis paths (tdd_chain RED/GREEN/REFACTOR/SYNTAX via `generateTDDSubTasks`, `createDirectTask`, `createConfigurationTask`, `createAggregationTask`); byte-for-byte Test Command equality when present; Build Command + non-vacuity wrapper when missing/empty (wrapper embeds Build Command verbatim, has `&&` tail, tail ≠ `true`); synthesis must not throw on command-less requirements; 8 fixture PRDs + inline Test-Command fixture corpus via `analyzePRDDeterministic`.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `node .agents/skills/task-breakdown/test-verification-command.mjs` — FAIL 9/9 (rc from runner report `# fail 9`), all for the RIGHT reason: `metadata.verification_command` is `undefined` on every synthesized bead (feature not implemented) and no fenced description block exists. Sample failure: `task-fr-001-red must carry the requirement Test Command byte-for-byte / + actual - expected / + undefined`. No compile/typo errors.
- No implementation was made in runner.mjs (RED discipline).

## Contract GREEN must implement
- `verification_command` = req.testCommand byte-for-byte when non-empty; else `<buildCommand> && <non-vacuous tail>` (tail present, non-empty, not the bare `true`).
- Description must carry `**Verification Command:**` fenced block whose content equals metadata byte-for-byte; extraction regex pinned in the test: `/\*\*Verification Command:\*\*\s*\n+```[^\n]*\n([\s\S]*?)\n```/`.
- Applies to all four paths: generateTDDSubTasks, createDirectTask, createConfigurationTask, createAggregationTask.

## Open edges for the next phase
- Analyzer parses Test Command only from inline `**Test Command:** <cmd>` lines (not fenced blocks) — the inline fixture in the test follows that convention; the PRD's fenced-block format is closer-side, not analyzer-side.
- Wrapper tail content beyond the pinned shape (embeds build command, `&&`, non-empty, ≠ `true`) is GREEN's choice — tests deliberately don't over-pin it.
