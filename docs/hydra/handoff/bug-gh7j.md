# bug handoff

## What changed (files + one line each)
- internal/core/integration_base.go: `originDefaultBranch` resolves remote truth first via `git ls-remote --symref origin HEAD` (15s timeout, read-only, fail-open) before local `refs/remotes/origin/HEAD` symref and development/main candidates; new helper `remoteSymrefDefault`. Fixes fms stale-symref PR targeting (origin/HEAD -> main, real default development, PR #69 dead target).
- internal/core/integration_base.go: successful ls-remote opportunistically heals the stale local symref via `git remote set-head origin <branch>` (best-effort, logged) — covers the set-head --auto alternative as a lazy self-heal.
- internal/core/integration_base_test.go: 3 new tests — remote truth beats stale local symref; unreachable remote falls back to local symref; reconciliation of stale local symref. `TestResolveIntegrationBase_SymbolicRefHead` updated (remote HEAD also repointed to trunk since remote advertisement now wins).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestResolveIntegrationBase' -count=1` — PASS (7 tests incl. 3 new).
- `gofmt -l` on changed files — clean.
- Commits: ace73e5 (ls-remote remote-truth + fallback), 75e4f3a (reconciliation self-heal), verified via `git show --stat`.

## Open edges for the next phase
- Telematics same-class fix: verify its hydra.yaml integration path picks up development after deploy (watch `[IntegrationBase]` log lines on next publish).
- ls-remote runs per ResolveIntegrationBase call — if latency on large remotes becomes a problem, cache the resolved default per project.
- Watch for `[IntegrationBase] ls-remote --symref origin HEAD failed` log lines on offline hosts (expected fail-open).
