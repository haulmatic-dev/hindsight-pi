# green handoff (hydra-q98z)

## What changed (files + one line each)
- .agents/skills/task-breakdown/runner.mjs: Verification Command contract — new `buildVerificationCommand` (Test Command byte-for-byte; else Build Command + non-vacuity `&& echo "verification: non-vacuity gate — command completed with exit 0"` tail; else tail alone), `verificationCommandBlock` (machine-parseable `**Verification Command:**` ```sh fenced block), `applyVerificationContract`; wired into `generateTDDSubTasks` (all phase beads), `createDirectTask`, `createConfigurationTask`, and `createAggregationTask` (all 9 createTask sites via local `_aggCreateTask` wrapper).
- .agents/skills/task-breakdown/runner.mjs: configuration_task now emits ONE fused APPLY bead (SYNTAX validation steps folded into apply instructions; labels gain tdd-syntax/quality) — pinned by the test's `created.length === 1` contract and aligned with the existing "CONFIG FUSION: Single Apply bead" design comment.
- .agents/skills/task-breakdown/runner.mjs: aggregation red description tolerates missing `req.dependsOn` (`(req.dependsOn || []).join`).
- .agents/skills/task-breakdown/analyzer.mjs: `analyzePRDDeterministic` now exposes top-level `route` (test 8 asserts `analysis.route`) computed via new non-recursive `computeRoute(content, requirements)`; `routeTaskPattern` refactored to share it. Fixes latent infinite recursion had routeTaskPattern been called inside analyze (routeTaskPattern itself calls analyzePRDDeterministic).
- .agents/skills/task-breakdown/test-verification-command.mjs: one-character test typo fixed — assert message referenced `sub.localId` but the loop variable is `t` (ReferenceError made test 7 unrunnable; behavior contract unchanged).
- CHANGELOG.md: Added entry under Unreleased.
- Merged RED strand hydra/wt-hydra-ax6z (commits 12a071c, 0c53d6d, f42c057) into this branch before implementing.

## Verification (commands run + pass/fail)
- `node test-verification-command.mjs` — PASS: `# tests 9 / # pass 9 / # fail 0`.
- `VERIFICATION_COMMAND_ENFORCE=1 node test-verification-command.mjs` — PASS: 9/9, fail 0 (RED gate satisfied).
- `node test-pattern-routing.mjs` / `test-preflight.mjs` / `test-crosscutting.mjs` / `test-shape-classification.mjs` / `test-direct-task.mjs` — ALL PASS, rc=0 (no regression from analyzer change).
- `node --check runner.mjs` + `node --check analyzer.mjs` — SYNTAX OK.
- `npx @biomejs/biome lint` on runner.mjs/analyzer.mjs/test-verification-command.mjs — clean.
- Coverage (c8 over test-verification-command run): new helper region (runner.mjs 1600–1719) fully covered — only uncovered lines are pre-existing `extractLocationFromPoint` (1665–1670); whole runner.mjs 36.98% (pre-existing paths not exercised by this suite), analyzer.mjs 54.88%.
- Commit 779d6af verified via `git show --stat` (4 files, +108/−29).

## Open edges for the next phase
- Config path behavior change: one bead per configuration requirement instead of two (apply+syntax) — downstream watchers of `${baseId}-syntax` config localIds (none found in repo) should be re-checked if any consumer appears.
- Wrapper tail wording is GREEN's choice per RED handoff ("tests deliberately don't over-pin it"); change freely within the pinned shape.
- Test 6 pins config synthesis to exactly 1 created bead — if a future requirement wants a separate syntax bead for configs, the test must be renegotiated first.
