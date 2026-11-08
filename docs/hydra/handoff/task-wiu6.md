# green handoff

## What changed (files + one line each)
- .agents/skills/task-breakdown/analyzer.mjs: new exported `TaskRoute` enum (exactly 8 members: analysis, test_authoring, refactor, spike, deletion, audit, legacy_no_scope, normal_code) + `routeTaskPattern(content)` → `{route, reason}`. Reuses `analyzePRDDeterministic` (files/type/req model) — no second parser; type markers (`**Type:** x`, case-insensitive) and shape-bearing header prefixes (TEST-001, REF-1, SPIKE-2, AUDIT-1, DEL-3, Analysis-*) take precedence; audit wins over mixed batches; plain FR/NFR/US reqs fall back to target-file heuristics (all unscoped → legacy_no_scope, any scoped → normal_code). Empty content → legacy_no_scope guard.
- CHANGELOG.md: Added entry under Unreleased.
- (cherry-picked onto this branch: RED commit 0b90a68 — test-pattern-routing.mjs + 8 fixtures — was not in HEAD; d4cd53a RED handoff.)

## Verification (commands run + pass/fail)
- `node .agents/skills/task-breakdown/test-pattern-routing.mjs` — PASS, 8/8 fixtures asserted, exit 0.
- Coverage (npx c8 --all --include=analyzer.mjs): new code (lines 349–453) 98/105 statements = **93.3%**; missing lines 400–401 (empty-input guard) and 447–451 (final fallback) — deliberate edge branches. Whole file 54.2% (pre-existing code largely uncovered by this suite).
- `node test-crosscutting.mjs` — FAIL, pre-existing (unhandled throw at analyzer.mjs:331 target-file overlap); verified identical failure with changes stashed → NOT caused by routing work. Flagged for its own chain per RED handoff.
- `node test-preflight.mjs` — 1 pre-existing failure ("expected lint:skipped event or line when prd-lint is missing"); identical with changes stashed. FR-002 preflight pending, not this chain.
- No placeholder comments in analyzer.mjs new code; no TODO/FIXME introduced.

## Open edges for the next phase
- Runner wiring (runner.mjs consuming routeTaskPattern) left unwired — RED handoff marked it GREEN's discretion; smallest-change discipline applied. If scheduling should act on routes, wire in a follow-up.
- test-crosscutting target-overlap crash and test-preflight lint:skipped failure are separate chains' RED work (unmodified by this bead).
