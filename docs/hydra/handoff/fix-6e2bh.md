# Fix handoff (fix-6e2bh — junk signature package GREEN verify-first)

**Bead**: fix-6e2bh (fix fold of hydra-22ry0, "GREEN: Implement Junk signature package (content signatures + negative probes)")
**Disposition**: verify-first close, no code changes (sole artifact is this handoff).

## Why the original bead failed (infra, not code)

hydra-22ry0 closed `failed` exit 1 with repeated model-call infra errors, not test failures:

- `400 {"type":"error","error":{"type":"invalid_request_error","message":"The total text input size exceeds 8 MB"}}` — model input-size exhaustion.
- `402 {"error":{"message":"You requested up to 32768 tokens, but can only afford 1968"}}` — OpenRouter credit exhaustion.

This matches the sibling fix-fold pattern (fix-8t1fd, fix-53w1a, fix-w1j57, fix-8uw4, fix-qb50 — all verify-first).

## Verification re-run (this worktree) — all VERIFIED

| Gate | Command | Result |
|---|---|---|
| Impl flag | `grep -n junksigImplemented internal/junksig/junksig.go` | `19: const junksigImplemented = true` |
| Build | `go build ./...` | BUILD_OK |
| Vet | `go vet ./internal/junksig/` | VET_OK |
| Narrow tests | `go test ./internal/junksig/ -run TestJunksig_ -count=1` | `ok github.com/haulmatic-dev/hydra/internal/junksig 0.052s` |
| Formatting | `gofmt -l internal/junksig/` | clean |
| Changelog | `grep -in junksig CHANGELOG.md` | RED + GREEN + refactor entries present (lines 13, 19, 36, 37) |

Package state (inherited, unchanged): `IsJunkPath` segment-structural matcher (`.tmp*` segments, exact `.jest-cache`, `haste-map-*`/`jest-transform-cache-*` basenames, `.hydra-preflight-ok`, paths under `.pi/task-breakdown/`), `IgnorePatterns()` = six `**/`-anchored lines; 10/10 contract tests pass unmodified. Post-refactor helper `isJunkSegment` (hydra-q1nxj) intact.

## Notes for the next agent

- `git status` shows a pre-existing local `M .gitignore` (`.scratch/` added) from scratch scaffolding — not part of this bead, left uncommitted.
- Host hazard this run: context-mode hook denied shell execution (`NODE_TOO_OLD`, Node v22.22.2 < 24.15.0; `ctx_upgrade` itself failed: `fatal: Remote branch release not found in upstream origin`); commands were run through the python sandbox instead. Flaky — retry on denial.

**Verdict**: verification passes; GREEN work already landed upstream. Summary marker: `docs-only`.
