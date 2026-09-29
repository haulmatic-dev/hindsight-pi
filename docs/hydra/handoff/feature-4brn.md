# feature handoff (FR-031)

## What changed (files + one line each)
- internal/core/deploy.go: new Tier1Deployer — canary deploy pipeline (drain precondition → backup → build → atomic swap → restart → verify-deploy.sh → automatic rollback), typed deploy events per step, opt-in via `deploy.enabled` (default false, NFR-004). Also `ClassifyTier1` (FR-030-lite path classifier, fail closed → tier-1g) and `AutoMergeIfTier1` (squash-merge only when all touched paths tier-1 AND gh checks green).
- internal/core/config.go: added `Deploy DeployConfig` (yaml `deploy`) to Config.
- internal/core/overseer.go: `tier1Deployer` field; wired in NewOverseer when `deploy.enabled` (worker-count probe from GetActiveWorkerCount, maxWorkers pin/restore via stateController); publishOneProject triggers `go RunAfterMerge` on successful publish when project.Path == deploy.repo_path.
- internal/core/deploy_test.go: 6 tests — classifier matrix, workers-in-flight skip (no backup created), successful swap, rollback on verify failure, disabled-inert, auto-merge fail-closed matrix.
- CHANGELOG.md: Added entry under Unreleased.
- docs/operations/host-setup.md: `deploy.enabled` knob row added.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (exit 0).
- `go test ./internal/core/ -run 'TestClassifyTier1|TestDeploy|TestAutoMergeIfTier1' -count=1` — PASS (6 tests).
- `go test ./internal/core/ -count=1` — PASS (full package, 55.6s).

## Open edges for the next phase
- Tier classifier is the path-based FR-030-lite subset; the full FR-030 classifier (subsystem ownership, test coverage delta) can replace ClassifyTier1 internals without changing call sites.
- AutoMergeIfTier1 is exported but not yet invoked from the publish flow — PublishViaPR already merges immediately when `output.pr_merge: true`; wiring the tier-1 gate ahead of that merge is the natural follow-up.
- Deploy events (`deploy_started`, `deploy_binary_swapped`, `deploy_verified`, `deploy_rolled_back`, `deploy_skipped_workers_in_flight`, …) are not yet surfaced in the fleet digest — digest reader may want a deploy section.
- Rollback restart assumes systemd unit name `hydra`; no config knob for the unit name.
