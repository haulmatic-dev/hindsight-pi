# red handoff (task-moo5, bead hydra-kzlu)

## What changed (files + one line each)
- tools/turn-report/main.go (new): RED-phase inert stub for the turn distribution extractor + budget calibration report — `eventTypeWorkerTurns` ("worker_turns"), `turnReportImplemented = false`, inert `run(args []string, stdout io.Writer) int` (prints nothing, exit 0, never refuses), main() calling run so the package compiles. Zero behavior until GREEN.
- tools/turn-report/main_test.go (new): 8 enforce-gated contract tests (`TURN_REPORT_ENFORCE` env, established codebase RED pattern; skip loudly by default so the daemon gate stays green, FAIL with "RED: turn-report contract not yet implemented (TURN_REPORT_ENFORCE=1)" when enforced) pinning ALL parent ACs:
  - Per-phase percentiles p25/p50/p75/p90/max, nearest-rank definition pinned twice (uniform sample + uneven sample [1..10] → 3/5/8/9/10).
  - Suggested `turn_budgets:` yaml block emitted as COMMENT lines only (#-prefixed), one entry per phase, value = round-up-to-10 of phase p90 (47→50, 301→310); never applied to any config; assertion that budget lines are comments.
  - Zero events → `insufficient data: N events` exit 0 (fail-open reporting); N counts only worker_turns events inside the --days window (unrelated in-window events still yield 0).
  - Drift warning when a phase sample < 20 (19-sample phase warned with count, 20-sample phase not).
  - `--days N` window filter (30-day-old worker_turns event excluded from distributions and from the calibration).
  - Type filtering: only `worker_turns` events feed distributions; `bead_closed` with turns/phase fields ignored.
  - `TURN_REPORT_NOW` env is the established injectable-clock seam for deterministic window tests.
  - Event contract pinned: `{"type":"worker_turns","bead":...,"fields":{"turns":int,"phase":string}}` on the daemon Event shape (internal/core/events.go; Phase default "unspecified", mirrors scripts/turn_economy.py's UNSPECIFIED).

## Verification (commands run + pass/fail)
- `go vet ./tools/turn-report/` — PASS (clean).
- `gofmt -l tools/turn-report/` — clean.
- `go test ./tools/turn-report/ -count=1` — PASS (ok, exits 0; all 8 contract tests skip loudly pre-GREEN → parent AC exits 0 satisfied).
- `TURN_REPORT_ENFORCE=1 go test ./tools/turn-report/ -count=1` — FAIL with exactly 8 `--- FAIL` lines, each `RED: turn-report contract not yet implemented (TURN_REPORT_ENFORCE=1)` — RED confirmed: missing-feature failures, not compile errors (package compiles + vets clean).
- `go test ./tools/turn-report/ -count=1 -cover` — PASS, coverage: 0.0% of statements — VERIFIED and REPORTED. The only non-test statement is the inert RED stub (`run()` body), which the skip-gated suite deliberately does not execute pre-GREEN; contract-test coverage of the real implementation is GREEN's obligation, measured with `-cover` against the pinned tests.

## Contract GREEN must implement
- Flip `turnReportImplemented = true`; implement `run(args, stdout)` covering every pinned test: parse `--events-dir DIR --days N` (+ `--json` optional), read `<dir>/<YYYY-MM-DD>.jsonl` day files, filter type=="worker_turns" and time within window (now = `TURN_REPORT_NOW` env override, else time.Now UTC), group `fields.turns` by `fields.phase` (missing → "unspecified"), print `p25=.. p50=.. p75=.. p90=.. max=..` per phase (nearest-rank over sorted counts), include `drift` warning naming phase + sample count when n<20, append `#`-commented `turn_budgets:` block with roundUp10(p90) per phase, print `insufficient data: N events` and exit 0 when no windowed worker_turns events exist. Exit 0 on success, never write any config.

## Open edges for the next phase
- `worker_turns` events are not yet emitted by the worker/daemon (grep: 0 producer hits); the tool's data source only exists once a future bead wires the report into the close path or worker exit. GREEN for this bead should implement the tool + tests only; emission wiring is a separate concern.
- `days` pinned only for 7; boundary semantics (inclusive "today" vs strictly older-than-days) is implied by the test (30-day-old excluded), GREEN may choose inclusive-window.
