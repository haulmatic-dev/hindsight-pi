# implement handoff

## What changed (files + one line each)
- internal/core/publish_identity.go (new): `PublishIdentity`, `ResolvePublishIdentity` (bead → project → owning user → `/home/<user>/.hydra/.github-token`, fail-closed via `ErrPublishIdentityMissing`), `ResolveDaemonPublishIdentity` (service account "hydra": owner PAT file → GH_TOKEN → daemon ~/.git-credentials — sanctioned only for hydra-owned beads), owner-aware `autoPRResolveToken(owner)`, git-author resolver (`resolveGitAuthorFor`, service-account default).
- internal/core/publisher.go: silent daemon-token fallback removed from `PublishViaPR` + `publishBranchViaPRCreate` (empty caller token → identity resolver → fail-closed error); `maybeOpenAutoPR` resolves owner identity, fail-closed (returns false = integration fails); `prMergeResolveToken` var deleted; old autoPRResolveToken body moved to `parseGitCredentialsToken` (daemon store only).
- internal/core/bead_closer.go: `ownerTokenForPublish` — resolves owner token, emits `publish_identity_resolved` / `publish_identity_missing` (repo-mirrored) for every close-path publish; both PR publish sites (PublishViaPR close + PR-first close) fail closed on missing identity.
- internal/core/overseer.go: `SetPublishIdentityResolver` + `SetPublishGitAuthorResolver` wired in NewOverseer (project → UserContext via getUserContextForProject); `publishOneProject` resolves identity fail-closed and emits typed events per publish tick.
- internal/core/worker_env_builder.go: worker env carries `GIT_AUTHOR_NAME/EMAIL` + `GIT_COMMITTER_NAME/EMAIL` from owner git identity — agent commits authored as bead owner.
- internal/core/worktree_manager.go: `ensureWorktreeLocked` pins `git config user.name/user.email` in every new worktree from owner identity.
- internal/core/integration_retest.go: `tokenFn` now `func(projectPath) (string, error)` via identity resolver; `land()` fails closed on missing identity.
- internal/core/publish_identity_test.go (new): PAT file read, owner fail-closed, daemon GH_TOKEN path, PublishViaPR fail-closed, git-author resolution, worker env identity.
- publisher_test.go / publisher_rebase_test.go: `prMergeResolveToken` injections replaced with `publishIdentityResolver`.
- CHANGELOG.md, docs/operations/host-setup.md §11: operator provisioning requirement (`/home/<user>/.hydra/.github-token`, 0600).

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ ./internal/... ./cmd/worker/ -count=1` — PASS (0 FAIL lines across all packages).
- Narrow: `go test ./internal/core/ -run 'TestReadOwnerPAT|TestResolvePublishIdentity|TestResolveDaemonPublishIdentity|TestResolveGitAuthor|TestWorkerEnvBuilderGitIdentity|TestPublishViaPR|TestPublishBranchViaPR|TestAutoPR|TestMaybeOpenAutoPR'` — PASS (20 tests).

## Open edges for the next phase
- LIVE VERIFICATION NOT DONE: telematics close → PR author login == ajantha; commit author == ajantha; zero service-token API calls. Requires the owner PAT files provisioned (`/home/ajantha/.hydra/.github-token`) — currently absent on this host → first user-owned publish will fail closed with `publish_identity_missing` (by design). Provision before deploy.
- `getUserContextForProject` matches project path exactly; projects whose stored path differs (trailing slash) resolve to daemon owner — acceptable (hydra-owned fallback), but worth watching.
- Beads filed by user A on user B's project publish under B (project owner), not A (bead filer) — operator rule keyed on project owner.

## Live verification addendum (2026-09-29, run as hydra user)
- `/home/ajantha/.hydra/.github-token` exists, mode 640, ACL `user:hydra:r--` → daemon-readable. VERIFIED.
- `ResolvePublishIdentity("/home/ajantha/projects/srv-telematics", LoadUserConfig("/home/ajantha"))` → owner=ajantha, source=owner-pat-file, git_author="Bili <ajantha@swyftflo.ai>". VERIFIED (live probe via go run).
- Owner PAT is DEAD: `GH_TOKEN=$(cat /home/ajantha/.hydra/.github-token) gh api user` → `401 Bad credentials` (twice). Control: daemon service token → `buddhiDsilva` (200). Any publish by ajantha will now fail closed at the GitHub call until the owner provisions a fresh PAT.
- Live AC (telematics close → PR author == ajantha) BLOCKED on operator credential renewal — environmental, not code. nipuna.j also has NO .github-token (will fail closed with publish_identity_missing when their beads publish).
