# green handoff

## What changed (files + one line each)
- .agents/skills/task-breakdown/analyzer.mjs: exported `TaskRoute` + `routeTaskPattern` (detection precedence: `**Type:**` markers → `### Spike-###`/`# Analysis:` headers → `**Target Files:**` scope for fr/legacy split), and `validateFileOverlap` now treats a shared file as acceptable when cross-cut serialization declared one claimant dependent on the others.
- .agents/skills/task-breakdown/preflight.mjs: removed machine-wide `/opt/dev-tools/.../prd-lint` fallback from `resolvePrdLintBinary` (test-preflight #4 contract: missing must stay distinguishable from installed-elsewhere).
- .agents/skills/task-breakdown/fixtures/*.md (8) + test-pattern-routing.mjs: restored from RED commit 778813d (existed on a prior loop ref, not in HEAD ancestry).
- CHANGELOG.md: GREEN entry under Unreleased.

## Verification (commands run + pass/fail)
- `node test-pattern-routing.mjs` — PASS, 8/8 fixtures asserted, exit 0.
- `node test-preflight.mjs` — PASS (was failing at HEAD before preflight fallback removal).
- `node test-crosscutting.mjs` — PASS (required validateFileOverlap serialization change).
- Coverage (NODE_V8_COVERAGE + c8): routeTaskPattern region 67/69 statements (97%), 17/18 branches (94%) covered; whole analyzer.mjs 50% (rest of file not exercised by fixture suite).

## Open edges for the next phase
- `runner.mjs` listed as target but needs no change — routeTaskPattern is analyzer-side; runner untouched (verified grep: no routeTaskPattern usage expected there).
- The two 1-of-18 uncovered branches: unreachable-empty-content guard paths — acceptable.
