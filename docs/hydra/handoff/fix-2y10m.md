# fix handoff (fix-2y10m, original hydra-igv5)

## What changed (files + one line each)
- docs/hydra/handoff/fix-2y10m.md: this handoff. Verify-first close — NO code change: the RED test suite for the Node dependency preflight close gate already exists, committed at HEAD (swept into 94eaf8df "chore: check Campaign-wide park via label sweep", 2026-10-29 18:24 UTC — the shared-worktree sweep carried this bead's files), and all tests pass against the implementation present in the worktree.
- Original bead hydra-igv5's exit-1 was pure worker infra, not a code defect: `❌ Pi rpc turn reached agent_end but the model call FAILED: Credential store read failed for llm-router: EACCES: permission denied, open '/opt/dev-tools/config/pi/auth.json'`. No lint/test finding was ever produced.

## Verified state
- `internal/core/gate_deps_preflight_test.go` (247 lines, tracked at HEAD, unmodified): 5 contract tests — install when package.json present + node_modules absent (install runs in worktree, verification runs last, output forwarded verbatim), no manifest → install skipped and exactly the verification command runs, install that dirties tracked files fails closed (tree-clean guard, verification never runs), --- PASS lines preserved verbatim in verification output (no `[no tests to run]`), existing node_modules → install skipped. RED skip-gate `gateDepsRequireImpl` + `GATE_DEPS_ENFORCE` env per the established codebase RED pattern.
- `internal/core/gate_deps_preflight.go` at HEAD is the RED inert stub (`depsPreflightImplemented = false`). The working tree carries an UNCOMMITTED flip to `true` + real implementation — a concurrent GREEN strand's work (same shared-worktree pattern as the pan_decision_packet/bead_closer uncommitted diffs). Not touched, not committed by this bead.
- Call-site wiring (`gateDepsPreflight` invoked from `gateExecutableAcceptance`) is NOT present — per the contract comment in gate_deps_preflight.go, that one-line call belongs to the FR-001 feature requirement's edit budget, not this RED bead.

## Verification (commands run + pass/fail)
- `go vet ./internal/core/` — PASS (clean, no output).
- `go test ./internal/core/ -run 'TestGateDeps' -count=1 -v` — PASS, 5/5 `--- PASS`, `ok github.com/haulmatic-dev/hydra/internal/core 0.048s`. No SKIPs: with the implementation present the tests execute real behavior (install sequencing, dirty-tree guard, verbatim output), not vacuous skips.
- `GATE_DEPS_ENFORCE=1 go test ./internal/core/ -run 'TestGateDeps' -count=1 -v` — PASS, 5/5, `ok ... 0.058s` — the enforce path (which fails instead of skipping on missing impl) holds against the implemented contract.
- `rg -n "gateDepsPreflight" internal/core/bead_closer.go internal/core/bead_closer_hard_gates.go` — 0 hits (wiring unclaimed, as expected for RED scope).

## AC mapping
- Tests written for Deps preflight pinning all parent criteria — MET (5 tests at HEAD, committed).
- Actual test run proving the suite — MET (default + ENFORCE both green, quoted above).

## Open edges for the next phase
- GREEN strand must commit the implementation flip (or re-apply if lost) AND add the one-line call inside `gateExecutableAcceptance` (bead_closer_hard_gates.go) so the preflight actually guards Node closes — currently the implemented function has zero production call sites.
- `detectNodeManager` (used by the uncommitted impl) must exist/land with GREEN — verify at that point.
