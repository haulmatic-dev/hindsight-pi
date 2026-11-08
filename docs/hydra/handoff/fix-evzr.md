# fix handoff (evzr)

## What changed (files + one line each)
- .agents/skills/task-breakdown/analyzer.mjs: (1) req-body `**Type:** <marker>` now captured as `req.typeMarker`; (2) new `applyTypeMarkerRouting(req)` called at the end of `classifyRequirement` — a recognized shape marker (analysis/audit/test/refactor/spike/deletion) overrides the switch to `direct_task` + needsWorktree=true, unknown markers (e.g. `fr`) leave classification untouched; (3) `isReqHeader` regex extended with TEST-/REF-/SPIKE-/AUDIT-/DEL-/ANALYSIS- prefixes so description accumulation stops correctly for those shapes.
- .agents/skills/task-breakdown/test-pattern-routing.mjs: classifier-wiring section — pins all 6 markers → `direct_task` via `analyzePRDDeterministic` and unknown-marker passthrough to `tdd_chain`.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- BEFORE repro (HEAD 1c23c48): `**Type:** analysis` on FR-001 → taskPattern `tdd_chain` (VERIFIED via node probe); shape-prefix ANALYSIS-001 → `direct_task` (worked).
- AFTER: markers analysis/audit/test/refactor/spike/deletion → `direct_task`; `fr`/`setup` markers → `tdd_chain` passthrough. PASS.
- `node test-pattern-routing.mjs` — PASS 8/8 fixtures + marker wiring, rc=0.
- `node test-preflight.mjs` rc=0; `test-crosscutting.mjs` rc=0; `test-shape-classification.mjs` rc=0; `test-direct-task.mjs` rc=0.
- `node --check analyzer.mjs` — PASS.
- Commit verified via `git show --stat` (3 files, +59/−1).

## Open edges for the next phase
- NFR with shape marker gets forced to `direct_task` (marker precedence strongest per §3.1a) — acceptable, but if a marker like `audit` should map NFRs to verification_gate instead, that needs a route-aware matrix.
- Acceptance-criteria ID-strip regex still lacks new prefixes (AC lines prefixed `TEST-001:` retain the prefix).
