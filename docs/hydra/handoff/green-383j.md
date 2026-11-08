# green handoff (hydra-383j)

## What changed (files + one line each)
- internal/beads/client.go: `closeBeadOnce` — on an open-children/blocked-by skip, fetches the bead; epics are NEVER force-closed through the skip (`refusing to force-close epic <id>: br reports open children (<verbatim skip>)`); terminal beads pass through to the existing terminality verify; a non-epic still takes the `--force` retry; skip with unverifiable bead type fails loud (fail-closed).
- internal/beads/client_test.go: mockExecutor gains `responseQueues` (`AddResponses`, FIFO per command key, falls back to the single-shot map) so one `br show` key can serve the skip-type verification and the terminality verify in sequence; the three skip-path tests updated to 4 calls; 2 new tests — epic open-children skip refuses force-close (verbatim wording + zero `--force` calls), exit-3 ambiguous skip on a terminal epic succeeds via terminality verify without force.
- internal/core/overseer.go: `persistClosedEpics` classifies a CloseBead error containing "open children"/"Skipped" as an `epic_close_refused` event (reason `open_children`, verbatim br refusal in Detail) and records the refusal (10m throttle) instead of logging a generic persistence failure.
- internal/core/epic_close_guard_test.go: `TestPersistClosedEpics_BrOpenChildrenRefusal_ClassifiedAsRefusal` — child WITHOUT the epic dep (link failed) is invisible to DAG+guard, close proposed, br refusal error → refusal recorded + typed event on the stream.
- CHANGELOG.md: Fixed entry under Unreleased.

## Root cause (chain)
Compile-time epic-child dependency links failed (br single-parent constraint) → children carry no `Dependencies` entry for the epic → (1) DAG `CloseReadyEpics` sees a vacuously-empty child set, (2) `epicHasOpenDependent` store scan (Dependencies-based) passes vacuously, (3) `closeBeadOnce` force-retried br's own `Warning: Skipped ...: parent issue has N/M open children` with `--force` — the only remaining ground truth — closing the epic over the open children (srv-jobfile: 33).

## Verification (commands run + pass/fail)
- `go build ./internal/beads/ ./internal/core/` — PASS.
- `go vet ./internal/beads/ ./internal/core/` — PASS (clean).
- `gofmt -l` on touched files — clean (internal/beads/parser.go drift is pre-existing, untouched).
- `go test ./internal/beads/ -count=1` — PASS (rc=0).
- `go test ./internal/core/ -run 'TestPersistClosedEpics' -count=1` — PASS (rc=0, 6 tests incl. new).

## Open edges for the next phase
- `epicHasOpenDependent` still cannot see children whose only link would have been the failed dep; the defense-in-depth is now the br refusal + typed event, not the scan. A future enhancement could surface br's parent-child graph via a `br` listing (parent_id not parsed today).
- Skip-with-unverifiable-type now fails closed for ALL bead types (retry queue handles); if worker close paths ever hit persistent GetBead failures on a skip, that surfaces as retries, not silent force-closes — intended.
