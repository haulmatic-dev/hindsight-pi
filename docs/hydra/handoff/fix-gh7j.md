# fix handoff

## What changed (files + one line each)
- internal/core/integration_base.go: `originDefaultBranch` now resolves remote truth first via `git ls-remote --symref origin HEAD` (15s timeout, read-only, fail-open) before falling back to the local `refs/remotes/origin/HEAD` symref and the development/main candidates — fixes stale-symref PR-targeting (fms: local origin/HEAD -> main, GitHub default development, PR #69 targeted dead main). New helper `remoteSymrefDefault`.
- internal/core/integration_base_test.go: 2 new tests — remote truth beats stale local symref (remote HEAD repointed to main while clone symref stale at development → base=main); unreachable remote falls back to local symref (fail-open). `TestResolveIntegrationBase_SymbolicRefHead` updated to also repoint the remote's HEAD to trunk (remote advertisement now wins).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestResolveIntegrationBase' -count=1` — PASS (6 tests incl. 2 new).

## Open edges for the next phase
- Alternative remedy from the bead (run `git remote set-head origin --auto` at project discovery) NOT implemented — ls-remote covers it without mutating local refs; if ls-remote latency becomes an issue on big remotes, consider caching the resolved default per project.
- `ls-remote` runs on every ResolveIntegrationBase call; failure logs `[IntegrationBase] ls-remote --symref origin HEAD failed ...` and falls back — watch that log line for noisy unreachable remotes.
