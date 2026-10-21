# Discovery: Bounded close-hold on self-reported `done:false` (FR-004, bead rjkx)

## Status
IMPLEMENTED and LANDED at HEAD (commit `43b98187` "feat(worker): FR-004 bounded close-hold on self-reported done:false"). This discovery bead re-verified the requirement and acceptance criteria against the shipped implementation; no code change required.

## Where the feature lives
- `cmd/worker/close_hold.go` — `closeHoldFor`, `closeHoldCount`, `runCloseHold`, `closeHeldRecord`. Typed event `worker_close_held` with contract fields `{bead, hold_count, verdict_summary}` plus `forced` at cap.
- `cmd/worker/main.go` (~lines 518 and 533) — wired into both close paths (plain close and files+tests-look-complete close).

## Acceptance criteria vs implementation
1. `done:false` verdict → bead NOT closed; typed `worker_close_held` event `{bead, hold_count, verdict_summary}` emitted; worker continues/exits without closing. ✔
2. Hold count persisted as `close-hold-N` label (replacing the previous one — only one present at a time); at hold_count == 2 the worker closes anyway (fail-open) and the event carries `forced: true`. ✔
3. Verdict JSON that fails to parse is ignored — close proceeds exactly as today (fail-open). ✔ `done:false` WITHOUT a summary also fails open.
4. No existing close-on-success test changes behavior; `go test ./cmd/worker/ -run 'CloseHold' -count=1` exits 0 with new tests (close_hold_test.go: TestCloseHoldFor, TestCloseHoldCount, TestCloseHoldEventMarshals, real-`br` label persistence test), `go build ./...` exits 0. ✔

## Ambiguities identified (resolved by implementation)
- "continues or exits without closing" — implementation exits the run without closing; the retry loop re-spawns the bead. Correct interpretation.
- "close-hold-N label" — replace-not-append semantics chosen (only one hold label at a time) so count is unambiguous.
- Cap value — `maxCloseHolds = 2` constant in close_hold.go; AC says hold_count == 2 closes anyway, consistent.
- Follow-up landed later: `53b02bfc` resolved hydraHomeDir resolution so close-hold events emit correctly; `a233e65e`/`3e59d535` vet-clean discovery handoffs on the same strand.

## Open edges
- None blocking. `maxCloseHolds` is a constant, not config — making it configurable is optional hardening if a fleet needs a longer hold window.
