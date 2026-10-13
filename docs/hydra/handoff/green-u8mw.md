# green handoff

## What changed (files + one line each)
- .agents/skills/task-breakdown/analyzer.mjs: new `TaskPattern.DIRECT_TASK` ('direct_task'), `classifyTargetShape`/`applyShapeClassification` (all-markdown or all-test FR targets → direct_task + needsWorktree=true; mixed/no files → unchanged tdd_chain), header regex extended for TEST/REF/SPIKE/AUDIT/DEL prefixes (classify direct_task, needsWorktree=false), `extractSystemType` + `result.systemType` from PRD "**System Type:**" line, validateRequirements skips new aux types. Existing FR/NFR/US/US-INT/SETUP/Doc/QA behavior unchanged.
- .agents/skills/task-breakdown/runner.mjs: `direct_task` added to validPatterns schema list + pattern sort order; bead creation falls through to the default TDD-chain branch (enabling change only — direct_task bead-flattening not in scope of this bead's criteria).
- .agents/skills/task-breakdown/test-analyzer-shape.mjs (new): 20 checks covering all parent AC (systemType cli-tool/null, all-markdown direct_task+needsWorktree, all-test direct_task, mixed tdd_chain, 5 aux prefixes parse+direct_task, all existing prefixes parse identically incl. NFR verification_gate and code-target FR tdd_chain).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `node test-analyzer-shape.mjs` — PASS (rc=0, all 20 checks).
- `node test-crosscutting.mjs` — FAIL, but PRE-EXISTING: same failure on stashed baseline (validateFileOverlap throws in cross-cutting fixture; both baseline and with-change rc=1). Not a regression from this change.
- `node test-preflight.mjs` — FAIL baseline too (rc=1 both). Not a regression.
- `git show --stat HEAD` — commit 4 files verified.

## Open edges for the next phase
- Runner bead creation has no direct_task branch — direct_task requirements still get a RED/GREEN/SYNTAX chain at creation time. If the intent is single-bead direct tasks, that needs an explicit runner branch (separate bead).
- test-crosscutting.mjs and test-preflight.mjs fail at baseline (pre-existing; overlap gate vs stale fixtures) — repair candidates.
