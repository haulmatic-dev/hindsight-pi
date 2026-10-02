# bug handoff

## What changed (files + one line each)
- internal/core/worktree_manager.go: `embeddedGitRepos` (WalkDir, prunes .git/.beads/node_modules/vendor/dist/build/target/.venv/__pycache__, does not descend into detected repos) + `guardEmbeddedRepos` — removes the embedded repo's `.git` metadata before staging, emits typed `embedded_repo_detected{bead, path}` (reason `removed`/`remove_failed`), fail-open. `Commit` refactored to `commitLocked`; new `CommitBead(worktreePath, message, beadID)` attaches bead identity for the event.
- internal/core/bead_closer.go: close-path commit switched to `CommitBead(wt, msg, string(bead.ID))`.
- cmd/worker/main.go: REPO HYGIENE prompt line — never `git init`/`git clone`/`go mod init` inside a workspace subdir (agent-prompt hardening from the bead).
- internal/core/worktree_embedded_repo_test.go: 2 tests — real-git embedded repo stripped, files committed, event verified; scanner finds nested, skips vendor + root.
- CHANGELOG.md: Fixed-style entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestCommitBead_StripsEmbeddedGitRepo|TestEmbeddedGitRepos' -count=1` — PASS (both; reproduction of the exact exit-128 error before the root-descend fix).
- `go test ./internal/core/ -run TestWorktree -count=1` — PASS (full Worktree set incl. lifecycle/ensure/integrate).
- `go build ./...` — PASS. `go vet ./internal/core/ ./cmd/worker/` — PASS.

## Open edges for the next phase
- Guard runs only at Commit; if agents create embedded repos during multi-turn runs they are cleaned at close (sufficient — close is what dies).
- `autoCommitDirtyMain` (retained for tests/manual repair) does NOT call the guard; if a manual repair flow reuses it, wire `guardEmbeddedRepos` there too.
- Pan could later file meta-beads from `embedded_repo_detected` events if the pattern recurs.
