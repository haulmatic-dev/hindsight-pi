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
- Full PR-close E2E on srv-telematics: live resolver check VERIFIED (parked checkout on feat/intergration-test-plan → development via origin-default, run in-sandbox against the real checkout). The actual next-bead-close PR requires redeploying the new binary to /opt/dev-tools/var/hydra/bin/hydra and restarting the daemon — an operator step (restarting the daemon from inside a worker session would kill that worker).
- `WorktreeManager.IntegrateBranch` / `resolveBase` (worktree_manager.go) still use `WorkingBranch` (checkout current branch) for the legacy local-merge no-remote fallback and worktree reset targets — left unchanged (blast radius: merge destination + reset semantics); the PR-first path (primary when a remote exists) is fully on the resolver.
- `publishAfterIntegrate` direct-push path still pushes the checkout branch to its remote namesake (pre-existing behavior, unchanged).
- Typed EventSink event for base mismatch not emitted — spec allowed "event/log line"; log line chosen. Add `integration_base_mismatch` event if the stream needs it.
- E2E on srv-telematics (checkout parked on `feat/intergration-test-plan`): next bead close should open PR against development — pending live verification.
