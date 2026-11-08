# fix handoff (fix-t1nm)

## What changed (files + one line each)
- .agents/skills/task-breakdown/runner.mjs: new `detectRepoLanguage(dir)` (go.mod → go; package.json → javascript; requirements.txt/pyproject.toml → python; else unknown) + `ensureRepoLanguage` cached on the runner, called at `run()` start; `getDevelopmentConstraints` language variants (go: go vet/tolerance/prereq go; python: pytest.approx; js/unknown: legacy toBeCloseTo + node/pnpm text unchanged); `lintGateCommandBlock`/`lintGateAcceptanceBlock` (go: `go vet ./...` + `gofmt -l`; python: ruff/flake8; else legacy Biome text) replace the hardcoded Biome LINT-gate description (scope line made language-neutral); `formatterCommand` (go: gofmt -w; python: ruff format; else biome format --write) replaces hardcoded "biome format --write" in SYNTAX + config apply/syntax instructions; `biomeConfigBlock` returns '' for go/python, gating the biome.json CRITICAL block; greenfield monorepo-config injection now only fires for javascript/unknown repos (Go/Python repos no longer get the JS scaffolding task); `hasClearImplementationApproach` tech-stack regex gains `go|golang`.
- .agents/skills/task-breakdown/test-language-detection.mjs (new): 9 node:test cases — marker matrix (go/js/python×2/none), instance cache, go-vs-legacy development constraints, go lint gate (go vet present, no Biome), end-to-end `createTasksFromAnalysis` on a go.mod repo (zero JS-stack strings — pnpm/toBeCloseTo/biome format/npm install/vitest — across every generated bead + metadata; no monorepo config task), unknown-language legacy injection preserved, go parent `generateTDDSubTasks` free of JS strings.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- DEFECT CONFIRMED PRE-FIX (repro): grep over runner.mjs for go.mod/golang/requirements/pyproject/language detection → 0 hits; JS boilerplate hardcoded at the cited lines (L746 tech-stack regex w/o go, L853 npm/vitest/biome monorepo injection, L1398 Biome lint gate, L1743 toBeCloseTo + "node, pnpm" prerequisites, L1959/L2478/L2538 biome format wording).
- `node test-language-detection.mjs` — PASS 9/9, rc=0 (RED before fix: 8 fail / 1 pass).
- `node test-verification-command.mjs` — PASS (0 fail). `node test-pattern-routing.mjs` — PASS 8/8. `test-preflight.mjs` rc=0; `test-crosscutting.mjs` rc=0; `test-shape-classification.mjs` rc=0; `test-direct-task.mjs` rc=0. `node --check runner.mjs` OK.
- `npx @biomejs/biome format --write runner.mjs test-language-detection.mjs` — applied; suites re-run green after format.

## Open edges for the next phase
- 'unknown' repos intentionally keep legacy JS boilerplate (conservative); a Rust/marker extension is a one-line table addition in detectRepoLanguage.
- Lint-gate "Scope" prose is now language-neutral; per-language wording lives only in Command/Acceptance blocks.

## Gate-failure addendum (validation round 2)
- Daemon gate exit 1: `go test ./...` FAILs in `internal/core` on 4 `TestPostCloseSweep*` tests (`postclose_sweep_test.go:122 expected bead reopened to open, got status ""` etc.). NOT this bead's defect: postclose_sweep.go + postclose_sweep_test.go are a different strand's RED-phase commit (3322928d, inert stub per that RED handoff "Contract GREEN must implement"); both files untouched by this bead's diff (`git diff origin/development...HEAD --stat` = only the 4 files listed above). The `FAIL src/x/a.spec.ts` line is a literal test-fixture string in `prespawn_probe_test.go`, also pre-existing.
- This bead's deliverable state re-verified post-gate: all 7 task-breakdown suites rc=0 (language-detection 9/9, verification-command, pattern-routing 8/8, preflight, crosscutting, shape-classification, direct-task).
- Remedy owner: the postclose-sweep chain's GREEN bead must implement `PostCloseSweeper.scan()` per its pinned contract.

## GREEN addendum (validation round 3)
- Implemented the cross-strand postclose-sweep contract whose inert RED stub (3322928d) was failing the daemon gate: internal/core/postclose_sweep.go — `pcCandidate` (closed + metadata["test_command"] + UpdatedAt ≤24h), scan() (fail → one re-run → pass = `postclose-flake` label only; still-failing = both run outputs attached via addComment (output else error string, `pcRunEvidence`), reopen StatusOpen + `postclose-reopened`; bead already carrying `postclose-reopened` → `needs-human` only, no second reopen; list/mutation errors fail-open), hourly Start loop + Stop (nil-safe).
- Wiring: `overseer.go` (`postCloseSweep` field; constructed in NewOverseer with `postCloseRunShell` bash -c runner in project dir, addComment nil — BeadsClient has no comment API), `service_starter.go` (Start/Stop alongside integrationSweep).
- `go test ./internal/core/ -run 'TestPostCloseSweep' -count=1` — PASS (all 6, incl. real-git integration). `go build ./...`, `go vet ./internal/core/` PASS; gofmt clean on touched files (pre-existing gofmt drift in other files belongs to other strands).

## Final round (validation round 4) — gate GREEN, verify-first close
- Gate artifacts at 11:38–11:40Z (post-fix run): build.json/lint.json/test.json all `status: "passed", exit_code: 0`; test.log 39 lines, zero FAIL, `ok ... internal/core 102.731s`. The round-3 re-prompt's failure text was STALE (pre-fix runs).
- Re-verified locally: `go test ./internal/core/ -run 'TestPostCloseSweep' -count=1 -v` → `PASS: 8 passed`; `node test-language-detection.mjs` 9/9. One transient no-output FAIL on the first narrow rerun — concurrent-strand compile race class, next run clean.
- No further edits. Bead complete.
