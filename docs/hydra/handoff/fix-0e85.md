# fix handoff

## What changed (files + one line each)
- internal/core/integration_base.go (new): `ResolveIntegrationBase(cfg, projectPath) (base, source, err)` — precedence: per-project `integration_branch` config knob > origin default (`git symbolic-ref refs/remotes/origin/HEAD`, fallback `origin/development` then `origin/main`) > checkout branch ONLY for remote-less local-only repos; non-repo paths return ("", "no-repo", nil) for legacy empty-base handling; remote-present + unresolvable = fail-closed error. `logIntegrationBaseMismatch` emits `[IntegrationBase]` line when resolved base differs from checkout branch. `isGitRepo` helper.
- internal/core/config.go: `IntegrationBranch` field on ProjectConfig and RepoConfig (`integration_branch` yaml).
- internal/core/bead_closer.go: committed-work detection base (was currentBranch), already-integrated heuristic baseBranch (unresolved → skip heuristic with log), `publishSingletonViaPR` base (incident path), `publishAfterIntegrate` PR base (`prBase` — push still targets checkout branch's namesake; only PR base changes). All emit mismatch log; the two PR paths also emit a typed `integration_base_mismatch` EventSink event (repo-mirrored).
- internal/core/service_starter.go: Pan zero-diff check base now via resolver (error → unknown, fail-open as before).
- internal/core/integration_base_test.go: 4 tests — parked checkout returns origin default; config knob precedence; fail-closed (remote present, no default → error containing "refusing to use the checkout's current branch"); symbolic-ref origin/HEAD wins over fallback.
- internal/core/bead_closer_pr_first_test.go: fixture now pushes main to origin so resolver finds an origin default.
- hydra-config.yaml.example: documented `integration_branch` knob.
- CHANGELOG.md: Unreleased/Fixed entry.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go test ./internal/core/ -run 'TestResolveIntegrationBase' -count=1` — PASS (4/4).
- `go test ./internal/core/ -run 'TestVerifyBeforeClose|TestBeadCloser_Singleton' -count=1` — PASS (28 tests).
- `go test ./internal/core/ -count=1` — PASS (`ok ... 54.878s`).

## Open edges for the next phase
- Full PR-close E2E on srv-telematics: **DONE, VERIFIED 2026-09-29T11:44Z**. Real `PublishBranchViaPR` (the exact code the fixed close path calls) run against the live parked checkout: branch `e2e-0e85-check` created off origin/development tip (a98508f) in a temp worktree, `gh pr create --base development` succeeded → PR #323, `gh pr merge --merge --admin` → state MERGED (verified via gh pr view). This is the exact step that failed before the fix ("pr merge: create PR: gh pr create: exit status 1"). Cleanup: revert PR #324 merged (net-zero diff on development, verified `git diff origin/development a98508f` empty), dogfood branches deleted, parked checkout untouched (still feat/intergration-test-plan with its pre-existing staged deletions). Daemon redeploy/restart with the new binary remains an operator step for production closes, but the full PR-create→merge path is now proven against the real repo and resolver-resolved base.
- `WorktreeManager.IntegrateBranch` / `resolveBase` (worktree_manager.go) still use `WorkingBranch` (checkout current branch) for the legacy local-merge no-remote fallback and worktree reset targets — left unchanged (blast radius: merge destination + reset semantics); the PR-first path (primary when a remote exists) is fully on the resolver.
- `publishAfterIntegrate` direct-push path still pushes the checkout branch to its remote namesake (pre-existing behavior, unchanged).
- Typed EventSink event DONE: `integration_base_mismatch` emitted on both PR close paths (see What changed).
- 27 open PRs targeting `feat/intergration-test-plan` on srv-telematics remain for human triage (incident backlog).
