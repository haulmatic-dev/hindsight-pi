# fix handoff

## What changed (files + one line each)
- internal/core/publisher.go: `resolveOrCreatePR` gains the missing leg-1 pre-check — `gh pr list --head <head> --state open` runs BEFORE `gh pr create`; an open PR skips create and is reused (URL via existing `ghPRView`/`resolveExistingPR`), a list error fails open to the create path. Leg-2 (create "already exists" error → resolve via `prURLFromError`/`gh pr view`, fail-closed when unresolvable) and the bead_closer benign handling already existed on this branch and were verified, not changed.
- internal/core/publisher.go (tier policy): new `PRWaitingError` sentinel + `prAutoMergeGate`/`SetPRAutoMergeGate` + `mergeOrLeaveOpen(projectPath, base, ...)` — both create paths (`PublishViaPR`, `publishBranchViaPRCreate`) and `maybeOpenAutoPR`'s existing-open-PR branch admin-merge ONLY when the repo is allowlisted AND base == "development" (conservative tier policy 2026-09-29, until FR-030); nil gate = legacy always-merge (tests/unconfigured daemons); otherwise the PR is left open and a `PRWaitingError` surfaces.
- internal/core/config.go: `output.pr_auto_merge_repos []string` on OutputConfig + `Config.IsPRAutoMergeRepo(projectPath)` (deploy.repo_path implicitly allowlisted; nil/empty fails safe to no auto-merge).
- internal/core/service_starter.go: gate wired unconditionally at daemon Start from `o.config`.
- internal/core/bead_closer.go: `publishAfterIntegrate` + `publishSingletonViaPR` treat `PRWaitingError` (errors.As) as success — activity "PR open — waiting on human merge: <url>", close returns nil, no integration-failure reopen.
- internal/core/publisher_existing_pr_test.go: +7 tests (pre-check skip-create, fail-open, waiting sentinel, tier-1 merge, tier-1 non-development base waits, waiting passthrough through rebase retry, config accessor, product-repo close path succeeds without merge).
- hydra-config.yaml.example, docs/operations/host-setup.md: `output.pr_auto_merge_repos` knob documented (tier policy incl. base==development restriction).
- CHANGELOG.md: two Fixed entries under Unreleased.
- docs/hydra/handoff/fix-48yw.md: this handoff.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -count=1` (full package) — PASS (56.8s after tier tightening; 58.9s before).
- `go test ./internal/core/ -run 'TestResolveOrCreatePR|TestMergeOrLeaveOpen|TestConfigIsPRAutoMergeRepo|TestPublishAfterIntegrate_ProductRepoWaitingOnHuman|TestPublishBranchViaPRCreate_Waiting|TestDefaultAutoPRGhCreate_ResolveExistingPR|TestIsPRAlreadyExists|TestAutoPRGhMerge' -count=1 -v` — PASS (12 tests).
- `go test ./internal/pan/ -count=1` — PASS (no cross-package fallout).

## Open edges for the next phase
- Operator config: product-repo installs need NOTHING new (default = stop at PR-open); the hydra self-repo keeps auto-merge via its existing `deploy.repo_path`. Any other tier-1 repo must be listed under `output.pr_auto_merge_repos`.
- maybeOpenAutoPR's `pr-open` label bookkeeping: a waiting PR still gets the pr-open label flow; humans merging later is tracked by existing triage (internal/triage service).
- Live check on srv-telematics (25 open dogfood PRs): next close attempt for a bead whose PR exists must complete as landed/waiting, not integration-failed — requires a daemon redeploy with this binary.
