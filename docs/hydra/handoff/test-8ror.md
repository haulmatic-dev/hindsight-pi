# verify-first handoff (test-8ror)

## What changed (files + one line each)
- docs/hydra/handoff/test-8ror.md: this handoff. No code change — verify-first close.

## Verification (commands run + pass/fail)
- `go test ./cmd/worker/ -run 'CloseHold' -count=1` — PASS (1 passed).
- `rg -n 'CloseHold|close-hold' cmd/worker/close_hold.go cmd/worker/main.go` — implementation present (close_hold.go: FR-004 bounded close-hold, maxCloseHolds=2, close-hold-N label, worker_close_held event, fail-open on nil/malformed verdict).
- Prior handoff `docs/hydra/handoff/task-8uw4.md` confirms FR-004 implemented at HEAD (commit 43b98187) with 9 test funcs, coverage: closeHoldFor 100%, runCloseHold 100%.
- CHANGELOG.md already carries the FR-004 entry (Unreleased/Added + refactor closeWithHold dedupe entry).

## Conclusion
FR-004 bounded close-hold fully implemented and tested. All ACs (hold on done:false, close-hold-N label, forced at cap, fail-open on malformed verdict, no close-on-success change) MET. Verify-first close: no code change required this pass.

## Open edges for the next phase
- persistCloseHoldLabel br-CLI real-workspace coverage remains 21.4% (fail-open branches) — optional, not AC-required.
