# red handoff (task-ai97h)

## What changed (files + one line each)
- internal/core/worktree_manager_scratch_test.go (new): RED contract tests for scratch scaffolding — `TestScratch_DirCreatedOutsideTree` (EnsureWorktree scaffolds the scratch dir outside the worktree tree + injects literal `.scratch/` line into the worktree .gitignore), `TestScratch_PruneSites` (subtests per teardown path: `Remove`, `QuarantineBranchAndRemove`, `CleanupStaleWorktrees` — scratch exists pre-teardown as precondition, gone after; GC path backdates the worktree's files so the scratch prune must be explicit, not junk-dir cleanup), `TestScratch_EnvExported` (`WorkerEnvBuilder.Build` output contains `HYDRA_SCRATCH_DIR=<scratchDirFor(projectPath, bead.ID)>`). Gated on `scratchRequireImpl` — skip loudly by default, `SCRATCH_ENFORCE=1` fails them.
- internal/core/worktree_manager.go: inert RED stub — consts `scratchEnvVar` ("HYDRA_SCRATCH_DIR") + `scratchDirName` (".scratch"), gate var `scratchScaffoldingImplemented = false`, pure path derivation `(*WorktreeManager).scratchDirFor(projectPath, key)` = `<worktreeRoot>/.scratch/<sanitized key>`. Enough surface for the package to compile; zero behavior (no creation, no ignore injection, no pruning, no env export).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestScratch' -count=1 -v` — PASS, exit 0: `--- PASS: TestScratch_PruneSites` wrapper + all 5 leaf tests `--- SKIP` loudly with "RED: scratch scaffolding contract not yet implemented (worktree scratch dir + .gitignore injection + teardown pruning + HYDRA_SCRATCH_DIR export)"; no `[no tests to run]`; `ok github.com/haulmatic-dev/hydra/internal/core 0.393s`.
- `SCRATCH_ENFORCE=1 go test ./internal/core/ -run 'TestScratch' -count=1 -v` — FAIL, exit 1 — RED confirmed, every pinned test fails for the RIGHT reason (missing feature, same message above): `--- FAIL: TestScratch_DirCreatedOutsideTree`, `--- FAIL: TestScratch_PruneSites` (3/3 subtests), `--- FAIL: TestScratch_EnvExported`.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on both touched files — clean.

## Contract GREEN must implement (pinned API)
- EnsureWorktree: create `scratchDirFor(projectPath, key)` dir on creation; append `.scratch/` to the worktree `.gitignore` (mirror the `ensureGitignoreLine` dual-spelling pattern from provisionGraphifyOut if desired — test pins only the literal `.scratch/` line).
- Prune sites (all three): `removeWorktreeAndBranchName` (Remove path), `QuarantineBranchAndRemove`, `CleanupStaleWorktrees` — remove `scratchDirFor(projectPath, key)` with the worktree; scratch must be gone even when FRESH (younger than the GC window).
- `WorkerEnvBuilder.Build`: append `scratchEnvVar=<scratchDirFor(projectPath, string(bead.ID))>` (key = full bead ID, note `types.BeadID` needs the `string()` conversion).
- Flip `scratchScaffoldingImplemented = true`.
- worker_spawner.go / cmd/worker/main.go (target files): expected GREEN wiring sites for the env export if Build cannot derive it internally — no RED-phase change was needed there (both compile untouched).

## Open edges for the next phase
- `scratchDirFor` is derived from `worktreeRoot(projectPath)`, so it reads HYDRA_HOME at call time — env export and dir creation must resolve under the same HYDRA_HOME (worker and daemon share $HYDRA_HOME on live hosts; tests set t.Setenv where GC isolation matters).
- CleanupStaleWorktrees' junk-dir branch (no .git file, older than maxAge → RemoveAll) is an independent backstop for long-abandoned scratch dirs; do not rely on it for the pinned contract (fresh scratch must be pruned by the explicit teardown path).

---

# PRIOR PASS (same bead hydra-ai97h, junksig chain) — preserved handoff from the earlier strand on this branch (commit 3e5897fb; junksig CHANGELOG entry above is its record)

# red handoff (hydra-ai97h)

## What changed (files + one line each)
- internal/junksig/junksig.go (new): RED-phase inert stub — `const junksigImplemented = false`, `IsJunkPath(p string) bool` returns false, `IgnorePatterns() []string` returns nil, full contract doc comments. Package compiles, zero behavior.
- internal/junksig/junksig_test.go (new): 10 contract tests under `TestJunksig_`, all gated on `junksigRequireImpl` (`JUNKSIG_ENFORCE=1`): RealWorldIncidentPaths (observed incident junk matches), NegativeProbes (cache-suffixed code files, `template.tmp`, `snapshot.tmp.png`, in-code `task-breakdown` never match), StructuralNotSubstring (never substring matches), RenamedParentStillMatches (`renamed/.jest-cache/blob`, `renamed/.tmp-jest/b`), BasenamesSignatures (`haste-map-`/`jest-transform-cache-` prefixes, exact `.hydra-preflight-ok` basename any depth), HydraPreflightExact, PiTaskBreakdownPrefix (everything under `.pi/task-breakdown/`), IgnorePatternsReproduceMatcher (hermetic `git check-ignore` property test, `GIT_CONFIG_GLOBAL/SYSTEM=/dev/null`), CoverageResidualNotSignature (jest `coverage/` accepted residual recorded), IgnorePatternsWellFormed.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `gofmt -l internal/junksig/` — clean.
- `go vet ./internal/junksig/` — PASS (clean).
- `go build ./...` — PASS (BUILD_OK).
- `go test ./internal/junksig/ -count=1` — PASS, exit 0; 10/10 `--- SKIP` loudly ("RED: junk signature contract not yet implemented") → daemon gate stays green pre-GREEN.
- `JUNKSIG_ENFORCE=1 go test ./internal/junksig/ -count=1` — FAIL, exit 1; all 10 fail for the RIGHT reason: `RED: junk signature contract not yet implemented (IsJunkPath/IgnorePatterns are inert stubs)` — proves RED detects the missing feature, not a compile error.
- `go test ./internal/junksig/ -count=1 -cover` — `ok ... coverage: 0.0% of statements` (stub is inert and gated tests skip before invoking it; GREEN will lift this).

## Contract GREEN must implement
- `IsJunkPath(p string) bool` — segment-structural matcher: any path segment with prefix `.tmp`; directory segment `.jest-cache`; basename prefixes `haste-map-` and `jest-transform-cache-`; exact basename `.hydra-preflight-ok` at any depth; everything under a `.pi/task-breakdown/` segment. Never substring matching.
- `IgnorePatterns() []string` — gitignore-format lines that reproduce the matcher exactly (verified by the hermetic `git check-ignore` test).
- Flip `junksigImplemented = true`.

## Open edges for the next phase
- Verification command: `go test ./internal/junksig/ -run 'TestJunksig_' -count=1 -v` (default gate) and `JUNKSIG_ENFORCE=1 go test ./internal/junksig/ -run 'TestJunksig_' -count=1` (RED proof until implemented).

HYDRA_VERDICT recorded in final agent message.
