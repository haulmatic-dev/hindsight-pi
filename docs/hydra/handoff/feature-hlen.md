# feature handoff (hydra-hlen)

## What changed (files + one line each)
- internal/core/git_ownership_preflight.go (new): `gitAccessProbe` (injectable `git rev-parse --is-inside-work-tree` as the daemon/hydra user), `isDubiousOwnershipError` (exit-128 "dubious ownership" classifier), `gitOwnershipRemediation` (exact `git config --global --add safe.directory <path>` TEXT — never executed), `RunGitOwnershipPreflight` + `preflightGitOwnership` (per project/repo; dubious → typed `git_ownership_unusable` event + `SetProjectPark` auto-park with remediation in reason; other errors fail open), `noteSpawnGitOwnership` (spawn-time: `spawn_failed_git_ownership` event + `git-ownership` label, remediation in event detail and error).
- internal/core/spawn_preflight.go: `spawnPreflightGitAccess` — spawn-funnel check; dubious ownership → `spawn_blocked_git_ownership` typed event + `SpawnSuppressedError` (auto-resume once a human fixes trust).
- internal/core/overseer.go: git-access preflight wired into `spawnWorker` before the publish-identity preflight.
- internal/core/worker_spawner.go: EnsureWorktree failure branch classifies dubious-ownership first — distinct `git-ownership` class instead of generic `worktree-unavailable`.
- internal/core/service_starter.go: `go o.RunGitOwnershipPreflight()` at daemon start (next to credential preflight); `git-ownership` added to clear-hard-park clearable labels; `panBlockedReason` names the exact fix for the label.
- internal/core/scheduler.go, dag_navigator.go, charon.go: `git-ownership` treated as a durable hard-park label (retry-queue skip, reconcileOrphanClaim skip, DAG ready filter, orphan-reopen skip).
- internal/core/git_ownership_preflight_test.go (new): 6 tests — classifier, remediation text, simulated exit-128 → typed event + park with remediation string, fail-open on non-ownership errors, non-git skip, noteSpawnGitOwnership error contract.
- CHANGELOG.md, docs/operations/host-setup.md (git-ownership preflight section), docs/architecture/shepherd.md (3 event-table rows).

## Verification (commands run + pass/fail)
- `go build ./...` — PASS. `go vet ./internal/core/` — PASS (clean). `gofmt -l` on all touched files — clean.
- `go test ./internal/core/ -run 'TestIsDubiousOwnership|TestGitOwnershipRemediation|TestRunGitOwnershipPreflight|TestNoteSpawnGitOwnership|TestGetReadyBeads_SkipsWorktree|TestRecoverOrphanInProgressBeads_Parked' -count=1` — PASS (6 new + 2 regression).
- B1 repro: original condition = git exit 128 "detected dubious ownership" (fmsfe). BEFORE: `git grep git_ownership_unusable origin/development` → 0 hits (exit 1) — mechanism did not exist. AFTER: `TestRunGitOwnershipPreflight_DubiousOwnershipParksWithRemediation` simulates the exact incident stderr (`fatal: detected dubious ownership in repository at '/home/nipuna.j/projects/webapp'`, exit 128) and asserts typed event + park reason containing `safe.directory <repo>`. PASS. (Live uid-drop repro attempted: `setpriv --reuid 65534` → "setresuid failed: Operation not permitted"; stock git ignores GIT_TEST_ASSUME_DIFFERENT_OWNER — simulated-path tests are the reproduction per AC.)
- B2: `git show --stat HEAD` = 12 files, +359/−8; every claim above is in the diff. PASS.

## Open edges for the next phase
- `RunGitOwnershipPreflight` runs once at daemon start, like the credential preflight; it does not re-probe on a timer. If a repo's ownership is fixed mid-run, the project needs an operator unpark; per-bead `git-ownership` labels clear via `repair:clear-hard-park` (already supported).
- `spawnPreflightGitAccess` runs one `git rev-parse` per spawn (subprocess, fast); if spawn volume ever makes this hot, cache the probe result per repo with a short TTL.
- Real-host end-to-end (a genuinely other-user repo) NOT VERIFIED — no privilege-dropping capability on this box; live verification needs a host where the fmsfe-style repo exists.
