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
- RESOLVED 2026-09-29: `gh pr view 389` → `{"state":"MERGED"}` (human merged
  it). Remaining: redeploy `$HYDRA_HOME/bin/hydra` from current development
  (`scripts/verify-deploy.sh`).

## Part 2: host disk

- At bead start `/` was 99%. Recovery, all as uid `hydra`:
  - `systemd-tmpfiles --clean` ran (user-level perms only).
  - `go clean -cache` freed the daemon's 1.5G go-build cache.
  - Purged `/tmp/hydra-executions` files older than 24h (5,704 files >3d,
    plus 1,460 >1d — daemon execution-output JSON, already harvested into
    events/logs). **Largest single lever: ~6G.**
  - Net: **85% used, 68G free** — at the 85% watermark, no longer exceeding.
- Not doable as uid `hydra` (no sudo, verified `sudo -n true` → password
  required):
  - `journalctl --vacuum-size` / `journalctl --user --vacuum-size` — both
    "No journal files were opened due to insufficient permissions"
    (`/var/log` 1.7G, `/var/backups` 1.6G).
  - Cache caps: journald `SystemMaxUse`, ajantha npm cache purge
    (`/home/ajantha/.npm` 560M, permission denied). Hydra's own npm cache
    was cleaned (8.8M → 5.9M).
- **Operator follow-up (as root):** journalctl vacuum, set
  `SystemMaxUse=500M` in `/etc/systemd/journald.conf`, `npm cache clean
  --force` as ajantha. Optionally add a tmpfiles rule / cron for
  `/tmp/hydra-executions` retention (24h) so the ~6G does not re-accumulate.

## Verification

- `rg` confirms `resolveOrCreatePR`/`isPRAlreadyExists` present in
  `internal/core/publisher.go`.
- `go build ./...` — PASS.
- `go test ./internal/core/ -run 'TestResolveOrCreatePR|TestIsPRAlreadyExists|TestPRURLFromError' -count=1` — PASS.
- `df -h /` after cleanup: 86%, 65G free.
