# Publisher wedge triage — hydra-hqyb (2026-09-29)

## Part 1: publisher wedge (PR #389)

- Failure: `gh pr create` for `dogfood/publish-hydra → development` failed
  `a pull request for branch "dogfood/publish-hydra" into branch "development" already exists: https://github.com/haulmatic-dev/hydra/pull/389`.
- The code fix already exists and is committed on this branch (and landed on
  development):
  - `internal/core/publisher.go` — `isPRAlreadyExists` classifier,
    `prURLFromError`, `resolveExistingPR`, `resolveOrCreatePR`
    (create fails "already exists" → reuse the existing PR and merge).
    The reuse path explicitly cites PR #389 wedging the publish tick.
  - Commits: `b5eacb0 fix(publisher): resolve pre-existing PR instead of
    failing on gh pr create 'already exists'`, `d6c5752 hydra: integrate
    hydra/wt-hydra-6zwl (publisher PR-reuse reconciliation)`.
- Root cause of the live wedge: stale deployed daemon binary (same pattern as
  the mjws triage — publisher running pre-fix code).
- **Human action:** merge PR #389 (`gh pr merge 389 --merge --admin`), then
  redeploy `$HYDRA_HOME/bin/hydra` from current development
  (`scripts/verify-deploy.sh`).

## Part 2: host disk

- At bead start `/` was 99%; after `systemd-tmpfiles --clean` (run as user
  `hydra`) and concurrent external cleanup, usage is **86%** (368G used,
  65G free of 451G) — still above the 85% watermark.
- Not doable as uid `hydra` (no sudo):
  - `journalctl --vacuum-size=500M` — "No journal files were opened due to
    insufficient permissions" (`/var/log` is 1.7G).
  - Cache caps: journald `SystemMaxUse`, npm cache purge
    (`/home/ajantha/.npm` 560M, permission denied).
- **Operator follow-up (as root):** journalctl vacuum, set
  `SystemMaxUse=500M` in `/etc/systemd/journald.conf`, `npm cache clean
  --force` as ajantha.
- Biggest non-root consumers measured: `/opt/dev-tools/var` 9G (hydra runtime
  state — worktrees/branches cleanup is a separate lever),
  `/home/ajantha/.npm` 560M, `/var/log` 1.7G.

## Verification

- `rg` confirms `resolveOrCreatePR`/`isPRAlreadyExists` present in
  `internal/core/publisher.go`.
- `go build ./...` — PASS.
- `go test ./internal/core/ -run 'TestResolveOrCreatePR|TestIsPRAlreadyExists|TestPRURLFromError' -count=1` — PASS.
- `df -h /` after cleanup: 86%, 65G free.
