# green handoff (hydra-ai97h — junk signature package)

## What changed (files + one line each)
- internal/junksig/junksig.go: real matcher replacing the RED stub — `junksigImplemented = true`; `IsJunkPath` walks slash segments (basename = last segment) and matches structurally, never substrings: any segment with prefix `.tmp` (`.tmp`, `.tmp-jest`, `.tmp2`, and root scratch files like `.tmp-cov-full.txt`), the exact directory segment `.jest-cache`, basenames with prefix `haste-map-` or `jest-transform-cache-`, the exact basename `.hydra-preflight-ok` at any depth, and paths strictly under a `.pi/task-breakdown/` tree (segment after `task-breakdown` required — the dir itself is not junk, matching the gitignore line). Empty segments skipped; `filepath.ToSlash` normalizes separators.
- internal/junksig/junksig.go: `IgnorePatterns()` returns the six `**/`-anchored gitignore lines (`**/.tmp*`, `**/.jest-cache`, `**/haste-map-*`, `**/jest-transform-cache-*`, `**/.hydra-preflight-ok`, `**/.pi/task-breakdown/**`) — the leading `**/` component matches zero or more dirs, making every basename signature depth-independent, which is what makes the lines survive renames exactly like the matcher; no negations, no blanks (IgnorePatternsWellFormed guardrails hold).
- internal/junksig/junksig_test.go: NOT edited — RED suite already pinned the full contract; all 10 tests pass unmodified.
- CHANGELOG.md: GREEN Added entry above the RED entry.

Commit note: the GREEN implementation content landed in commit 276f5019 (the concurrent scratch-scaffolding strand's commit swept the worktree-staged junksig.go rewrite alongside its own RED files); this commit adds the GREEN changelog entry + this handoff on top.

## Verification (commands run + pass/fail)
- `go test ./internal/junksig/ -run 'TestJunksig_' -count=1 -v` — PASS, exit 0: 10/10 `--- PASS` (RealWorldIncidentPaths, NegativeProbes, StructuralNotSubstring, RenamedParentStillMatches, BasenameSignatures, HydraPreflightExact, PiTaskBreakdownPrefix, IgnorePatternsReproduceMatcher, CoverageResidualNotSignature, IgnorePatternsWellFormed); no `[no tests to run]`; `ok github.com/haulmatic-dev/hydra/internal/junksig 0.099s`.
- `go build ./...` — PASS (BUILD_OK, exit 0).
- `go vet ./...` — PASS (VET_ALL_OK, clean). `go vet ./internal/junksig/` — clean.
- `gofmt -l internal/junksig/` — clean.
- Coverage: `go test ./internal/junksig/ -count=1 -coverprofile` → 88.9% of statements (IsJunkPath 88.2%, IgnorePatterns 100.0%). Uncovered lines are defensive branches (empty path/segment, `.pi` as final segment) — negative probes cover the classification surface.
- Placeholder grep (`TODO|FIXME|// Placeholder`) over both target files — 0 hits; junksig.go 98 lines, junksig_test.go 295 lines, both non-empty.
- AC parent mapping: incident table PASS (AC1), negative table PASS (AC2), renamed-parent PASS (AC3), `IgnorePatterns()` ≡ matcher proven behaviorally via hermetic `git check-ignore` (GIT_CONFIG_GLOBAL/SYSTEM=/dev/null) PASS (AC4), coverage residual recorded PASS, ≥8 `--- PASS` + no `[no tests to` PASS (10).

## Open edges for the next phase
- None blocking. Consumer wiring (junk gate at publish/scope paths, FR-003+) is separate beads; this package is importable as-is (`github.com/haulmatic-dev/hydra/internal/junksig`).
- `.pi/task-breakdown` dir itself (no child segment) is deliberately not junk, consistent with the `**/.pi/task-breakdown/**` ignore line; if a consumer wants the bare dir matched, extend BOTH matcher and pattern together (property test enforces they stay in lockstep).

---

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
