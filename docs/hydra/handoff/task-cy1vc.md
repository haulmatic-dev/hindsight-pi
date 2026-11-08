# refactor handoff (cy1vc)

## What changed (files + one line each)
- internal/core/worktree_manager.go: removed the trivial `WorktreeManager.scratchDirFor` wrapper (one-line relay to `worktreeScratchDirFor`); single canonical path helper remains.
- internal/core/worktree_manager_scratch_test.go: deduped the repeated scaffolded-scratch precondition (stat after gate) into `requireScaffoldedScratch`; `TestScratch_EnvExported` now matches the exact computed `HYDRA_SCRATCH_DIR` entry instead of the first `HYDRA_SCRATCH_DIR=` line — the env builder starts from `os.Environ()`, so a daemon-set `HYDRA_SCRATCH_DIR` inherited by the worker process coexists with the builder-appended value (os/exec keeps the last duplicate, product behavior correct; the old first-match loop just found the inherited one and failed the test).
- CHANGELOG.md: Unreleased/Changed entry.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit baseline): `go test ./internal/core/ -run 'TestScratch_' -count=1 -v` — FAIL: only `TestScratch_EnvExported` failed (`HYDRA_SCRATCH_DIR=/opt/dev-tools/var/hydra/.hydra-worktrees/.../hydra-cy1vc, want /tmp/hydra-core-test-home-...` — inherited daemon env var vs computed). Other 3 tests PASS.
- AFTER: same command — PASS 4/4 (DirCreatedOutsideTree, PruneSites ×3 subtests, EnvExported).
- `go test ./internal/core/ -run 'TestWorktree|TestScratch_' -count=1` — ok (2.579s).
- `gofmt -l` on touched files — clean after `gofmt -w`.
- No benchmarks exist for the scratch path; none run.

## Open edges for the next phase
- `scaffoldScratch` also injects `.gitignore` into `<projectPath>/.git/info/exclude`; a root `.gitignore` was modified in this shared worktree (`+ .scratch/`) before this bead started — pre-existing, left untouched (uncommitted).
- Build starts from parent env by design; if a worker ever needs to suppress inherited HYDRA_SCRATCH_DIR explicitly, that is a new feature bead.
