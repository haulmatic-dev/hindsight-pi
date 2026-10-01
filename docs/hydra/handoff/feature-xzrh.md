# feature handoff

## What changed (files + one line each)
- internal/core/publish_identity.go: new `spawnPreflightPublishIdentity(o, bead, projectPath)` — spawn-funnel preflight; resolves the owner publish identity, blocks on `ErrPublishIdentityMissing` with typed `spawn_blocked_identity_missing` event (repo-mirrored via EmitRepo, log fallback); nil resolver / non-identity error / unresolvable project path fail open.
- internal/core/scheduler.go: preflight wired at both spawn sites — Tick retry-queue path (before `spawnWorker`, releases provider-pool slot on block) and SpawnWorkersForNodes DAG path. Bead re-checked next tick; auto-resumes when the PAT lands.
- internal/core/publish_identity_spawn_test.go: 5 subtests — nil resolver fail-open, resolved pass, missing blocks with ErrPublishIdentityMissing, transient resolver error fail-open, unresolvable project fail-open.
- internal/core/multi_user_e2e_test.go: test provisions alice's owner PAT (`~/.hydra/.github-token`) — the e2e previously relied on close-time (not spawn-time) identity checks; with the preflight live, alice must have a resolvable PAT for workers to spawn.
- CHANGELOG.md: Added entry under Unreleased.
- docs/architecture/shepherd.md: `spawn_blocked_identity_missing` row in the event-stream table.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run TestSpawnPreflightPublishIdentity -count=1` — PASS (5/5 subtests).
- `go test ./internal/core/ -run TestMultiUser_EndToEnd -count=1` — PASS (after PAT provisioning).
- `go test ./internal/core/ -count=1` — PASS (full package, 61s).
- gofmt — clean.

## Open edges for the next phase
- Preflight runs once per candidate bead per tick; no event throttle — a long-blocked bead emits `spawn_blocked_identity_missing` every tick (matches `spawn-deferred-target-overlap` behavior; add throttle only if stream volume becomes a problem).
- Preflight checks identity only; it does not validate the PAT against GitHub (close-path publish still fails closed on a revoked token).
- resolver is package-global (`publishIdentityResolver`); parallel tests that set it must serialize (existing pattern via SetPublishIdentityResolver(nil) cleanup).
