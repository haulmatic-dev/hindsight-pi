# bug handoff (bug-t55v)

## What changed (files + one line each)
- internal/core/postclose_sweep.go: `verificationCommandFor` now looks ahead past a "Test Command:" marker line for a fenced ```sh block (`fencedCommandAfter`) before falling back to same-line extraction; new `looksLikeSentence` rejects prose (dash-led bullets, sentence connectives) so extracted prose is never passed to `sh -c`.
- internal/core/verification_command_test.go (new): 6 tests pinning the contract — fenced block after prose marker line wins, prose-only descriptions yield no contract, dash-led prose rejected, metadata `test_command` still wins, backticked same-line still works, empty description yields none.
- CHANGELOG.md: Fixed entry under Unreleased.
- docs/hydra/handoff/bug-t55v.md: this handoff.

## Verification (commands run + pass/fail)
- REPRODUCTION (pre-fix, targeted probe): bead description `- Run the exact test command: npm test\n\n```sh\nnpm test``` ` → extracted cmd=`- Run the exact  npm test` (prose + stripped marker, no fence) → `sh -c` dies "Illegal option" exit 2 in 4ms without running tests. Bug confirmed.
- `go test ./internal/core/ -run 'TestVerificationCommandFor' -count=1 -v` — PASS: 8 tests (6 new + 2 pre-existing in suite), exit 0. Quote: `--- PASS: TestVerificationCommandFor_FencedBlockAfterMarkerLine` ... `--- PASS: TestVerificationCommandFor_EmptyDescriptionNoContract`.
- `go build ./internal/core/` — PASS. `go vet ./internal/core/` — PASS (clean). `gofmt -l` on touched files — clean.
- Full `internal/core` suite — **FAIL** after 600s timeout with stack trace at `database/sql/sql.go:841` (goroutine hang/panic). NOT attributable to this change (string-parsing only, no DB/goroutine involvement); environment then degraded (bash spawn EAGAIN on every command, 5+ consecutive failures) preventing re-run/commit.

## Open edges for the next phase
- Re-run full `internal/core` suite once environment recovers; if the sql.go:841 hang recurs, it is a separate pre-existing deadlock (suspect DB pool exhaustion in an integration test) — file separate fix bead, do NOT attribute to t55v.
- Commit outstanding: internal/core/postclose_sweep.go, internal/core/verification_command_test.go, CHANGELOG.md uncommitted at time of write due to shell EAGAIN.
