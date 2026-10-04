# refactor handoff (ank6)

## What changed (files + one line each)
- .agents/skills/task-breakdown/runner.mjs: removed the duplicated two-line pair (`const verificationCommand = this.buildVerificationCommand(...); const verificationBlock = this.verificationCommandBlock(...)`) at three call sites — `generateTDDSubTasks` phase loop (now `this.applyVerificationContract(sub, testCommand, buildCommand)` per sub), `createDirectTask` (inline `verification_command` metadata literal + manual description append replaced by one `applyVerificationContract` call after the object literal), configuration apply bead (same replacement). `applyVerificationContract` is now the single propagation path for the FR-001 Verification Command contract. Behavior byte-for-byte unchanged.
- CHANGELOG.md: Unreleased/Changed entry.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit baseline): `node test-verification-command.mjs` — 9 tests, 0 fail, rc=0.
- AFTER: `node --check runner.mjs` — OK. `node test-verification-command.mjs` — `# tests 9 / # pass 9 / # fail 0`, rc=0.
- All 6 suites: test-pattern-routing, test-preflight, test-crosscutting, test-shape-classification, test-direct-task — PASS; test-verification-command PASS.
- `npx @biomejs/biome format --write runner.mjs` — no changes (already formatted; biome.json scope covers .agents/**/*.ts, runner.mjs not in lint scope).
- One transient syntax error during the direct-task edit (metadata closing brace lost in replacement) — caught immediately by `node --check`, fixed before any test run.

## Open edges for the next phase
- No benchmarks exist for this functionality (pure task-emission code); no perf check run.
- Concurrent strand's uncommitted `internal/core/bead_closer_hard_gates.go` refactor (emitGateEvent helper) was present in the worktree and NOT touched or committed by this bead.
- No bugs found; nothing recorded via `cm add`.
