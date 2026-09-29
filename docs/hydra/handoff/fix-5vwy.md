# fix handoff (fix-5vwy)

## What changed (files + one line each)
- internal/beads/client.go: `closeBeadOnce` — a nonzero-exit `br close` skip (open children, exit 3 "already closed or skipped") previously returned nil ("idempotent success") without closing, without `--force` retry, and without terminality verification; now only explicit "already closed" wording is idempotent, worded/exit-3 skips take the same `--force` retry + terminality-verify path, and a still-skipped force close fails loud.
- internal/beads/client.go: `execInDir`/`execInDirOnce` no longer discard stdout on CLI error — output rides back in `CLIError.Stdout`, so the skip/"already closed" classifier in closeBeadOnce actually sees the wording (before this, EVERY errored close had empty output and only the ambiguous exit-3 shortcut could fire).
- internal/beads/client_test.go: 3 tests — nonzero-exit skip force-retries and closes; nonzero-exit with no skip/already-closed wording fails loud; "already closed" wording stays idempotent (no force).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go test ./internal/beads/ -count=1` — PASS (whole package, incl. the 3 new tests and existing force-retry tests).
- `go test ./internal/core/ -run 'TestClose|TestBeadCloser|TestVerifiedClose|TestTerminalGuard' -count=1` — PASS (18).
- `go vet ./internal/beads/` — PASS (clean).

## Open edges for the next phase
- Live: watch srv-telematics u8z7/kher/qj74/5gq — next close must land `br close` (or, if children genuinely block, a loud force-close error instead of silence); no respawn for already-landed beads.
- If a bead's open children are real blockers, the close now fails loud with "force-close still skipped" — visible via closeError, bead stays open; that is intentional (waiting state) but could loop a respawn per cycle; a durable "waiting-on-children" label is a possible follow-up.
- Other callers of execInDir now receive partial stdout with errors (previously nil); audit showed only closeBeadOnce inspects output on error — harmless for the rest, but a future error-path consumer could rely on it.
