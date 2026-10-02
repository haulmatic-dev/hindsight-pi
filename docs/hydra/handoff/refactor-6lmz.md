# refactor handoff

## What changed (files + one line each)
- .agents/skills/task-breakdown/analyzer.mjs: `routeTaskPattern` type-marker if-chain replaced with table-driven `TYPE_MARKER_ROUTES` (precedence order preserved; analysis/spike header signals handled in-loop); net −duplication, behavior identical.
- .agents/skills/task-breakdown/test-pattern-routing.mjs: 6 duplicated `console.error + failingFixtures.push` branches unified into one `fail(fixture, message)` helper; exit-1 + failing-names contract unchanged.
- CHANGELOG.md: Unreleased entry for the refactor.

## Verification (commands run + pass/fail)
- Baseline BEFORE edits: `node test-pattern-routing.mjs` PASS 8/8 exit 0; test-preflight rc=0; test-crosscutting rc=0.
- AFTER: `node test-pattern-routing.mjs` PASS 8/8 exit 0; preflight rc=0; crosscutting rc=0; empty-content route still `legacy_no_scope`.
- Negative contract: tampered expectation table (analysis→refactor) → all 8 fixture names printed + exit 1 (rc=1 VERIFIED).
- Commit: 1 commit, 3 files (+40/−37), verified via `git show --stat`.

## Open edges for the next phase
- runner.mjs target needed no change — routing is analyzer-side (matches green handoff finding).
- No benchmarks exist for this functionality; no perf check run.
- Untracked internal/prdcontract/ and modified create-prd/SKILL.md in worktree are pre-existing, not touched.
