# fix handoff (fix-frfe)

## What changed (files + one line each)
- internal/beads/client.go: cherry-picked aeb13bf — `closeBeadOnce` no longer treats a nonzero-exit `br close` skip (open children / exit 3) as silent idempotent success; only explicit "already closed" wording is idempotent, worded/exit-3 skips take `--force` retry + terminality verify, still-skipped force close fails loud.
- internal/beads/client.go: `execInDir`/`execInDirOnce` preserve stdout in `CLIError.Stdout` on error so the skip/already-closed classifier sees wording.
- internal/beads/client_test.go: 3 regression tests (nonzero-skip force-retries, no-wording fails loud, already-closed idempotent).
- CHANGELOG.md: Fixed entry under Unreleased (landed with cherry-pick).
- docs/hydra/handoff/fix-5vwy.md: original handoff restored.

## Verification (commands run + pass/fail)
- `go build ./...` && `go vet ./internal/beads/` — PASS.
- `go test ./internal/beads/ -count=1` — PASS (ok, incl. TestCloseBead_NonZeroExitSkip_ForceRetries).
- `go test ./internal/core/ -run 'TestClose|TestBeadCloser|TestVerifiedClose|TestTerminalGuard' -count=1` — PASS.

## Open edges for the next phase
- Commit aeb13bf existed only on dogfood/hydra-5vwy (diverged from development, not integrated); this worktree re-lands it as ab66a17. Integration should not merge dogfood/hydra-5vwy wholesale (diverged 4 commits behind/other content); take ab66a17.
- Live: watch srv-telematics u8z7/kher/qj74/5gq next close for loud failure vs close.
- Waiting-on-children durable label still a possible follow-up (per fix-5vwy handoff).
