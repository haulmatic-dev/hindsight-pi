# lint handoff (task-iwob)

## What changed (files + one line each)
- biome.json: formatter compaction (2→0-line includes array, final newline) — auto format fix, no semantic change.
- .agents/skills/systematic-debugging/condition-based-waiting-example.ts: formatter safe fixes (trailing commas after last params, multi-line template-literal reject call wrapping).
- internal/integration/.agents/skills/systematic-debugging/condition-based-waiting-example.ts: same fixes (mirrored copy).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `npx @biomejs/biome@2.3.14 lint .` — Checked 5 files, No fixes applied, exit=0. PASS.
- `npx @biomejs/biome@2.3.14 lint --write .` — no fixes needed. PASS.
- `npx @biomejs/biome@2.3.14 format --write .` — Fixed 3 files (all format-only). PASS.
- Post-fix lint rerun: exit=0. PASS.

## Open edges for the next phase
- None: lint gate green on the whole repo scope (biome.json files.includes covers **/*.js, **/*.ts, scripts/**/*.js, .agents/**/*.ts).
- graphify-out/graph.json absent in cwd → rg/config inspection fallback per protocol; hindsight disconnected at bead start (skip rule applies).
