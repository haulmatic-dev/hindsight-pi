# green handoff (task-sqv0c)

## What changed (files + one line each)
- internal/core/worktree_manager.go: GREEN for the scratch contract — `scratchScaffoldingImplemented = true`; package-level `worktreeScratchDirFor(projectPath, key)` = `<worktreeRoot>/.scratch/<sanitizeKey(key)>` (scratch dir lives OUTSIDE the worktree tree); `scaffoldScratch` (MkdirAll 0o755 + `ensureGitignoreLine(<wt>/.gitignore, ".scratch/")`, fail-open) called from `EnsureWorktree`; `pruneScratch` (os.RemoveAll, fail-open) wired into `removeWorktreeAndBranchName` and `QuarantineBranchAndRemove`; `CleanupStaleWorktrees` skips the `.scratch` root entry and explicitly removes the matching `<projectPath>/.scratch/<key>` alongside the stale worktree (prune is tied to teardown, not junk-dir age).
- internal/core/worktree_manager.go: scaffold-aware `worktreeClean` — untracked `.gitignore` counts as clean when its meaningful lines are exactly `scaffoldIgnoreLines` {".scratch/", "graphify-out/", "graphify-out"} (`scaffoldIgnoreOnly` helper). Without this, the injected `.gitignore` made `TestWorktreeManager_EnsureWorktree_ReusesValidWorktree` fail ("worktree not reset to base") because the untracked file kept `worktreeClean` false and blocked reuse-reset.
- internal/core/worker_env_builder.go: `WorkerEnvBuilder.Build` appends `HYDRA_SCRATCH_DIR=<scratchDirFor(projectPath, bead.ID)>` — the env builder is the single env surface; `worker_spawner.go` and `cmd/worker/main.go` needed no edits.
- internal/core/worktree_manager_scratch_test.go: NOT edited (RED contract is the spec).
- CHANGELOG.md: GREEN Added entry above the RED entry.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestScratch' -count=1 -v` (via compiled binary `/tmp/core.sqv0c.test -test.v` because the shell wrapper masks plain `go test` output) — PASS, exit 0: `--- PASS: TestScratch_DirCreatedOutsideTree`, `--- PASS: TestScratch_PruneSites` (subtests Remove, QuarantineBranchAndRemove, CleanupStaleWorktrees), `--- PASS: TestScratch_EnvExported` — 6 `--- PASS`, 0 `--- FAIL`, no `[no tests to run]`.
- `SCRATCH_ENFORCE=1` same run — PASS (RED gate now green both ways).
- Regression set `TestWorktreeManager|TestCleanupStale|TestScratch` — PASS, 0 FAIL.
- `go build ./...` — PASS (exit 0). `go vet ./internal/core/` — PASS (clean). `gofmt -l` on touched files — clean.
- WorkerEnvBuilder tests — PASS (env export surface).
- Coverage (TestScratch|TestWorktreeManager_EnsureWorktree, -coverprofile): `worktreeScratchDirFor` 100%, `scratchDirFor` 100%, `scaffoldScratch` 60%, `pruneScratch` 50%, `worktreeClean` 77.8%, `scaffoldIgnoreOnly` 80%.
- Placeholder grep (TODO/FIXME/Placeholder) over changed files — 0 real hits. Files non-empty (worktree_manager.go 1411 lines, scratch test 220 lines).

## Open edges for the next phase
- `pruneScratch`/`scaffoldScratch` error branches partially uncovered (50%/60% — fail-open paths).
- `scratchDirFor` method kept as a thin delegate over `worktreeScratchDirFor` for backward compat.
- Scratch dir is per-chain key at `<worktreeRoot>/.scratch/<key>`; no retention policy beyond teardown — a crashed worker leaves scratch until its worktree is GC'd (stale-worktree prune removes it alongside).
