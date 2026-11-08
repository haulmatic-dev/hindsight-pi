# green handoff (task-249m)

## What changed (files + one line each)
- tools/turn-report/main.go: GREEN implementation replacing the inert RED stub — `turnReportImplemented=true`; real `run()` parses `--events-dir` / `--days` (flag pkg, errors exit 2); scans dated `<YYYY-MM-DD>.jsonl` day files within the window (cutoff `now.AddDate(0,0,-days)`, time seam `TURN_REPORT_NOW` RFC3339), parses JSONL in a buffered scanner, counts only `worker_turns` events; per-phase (fields.phase, alphabetical) nearest-rank percentiles (`sorted[ceil(p/100*n)-1]`) printed as `  <phase> (n=N): p25=.. p50=.. p75=.. p90=.. max=..` plus a `warning: drift — <phase> sample size N < 20` line when n<20; suggested block printed as comment lines only: `# turn_budgets:` + `#  <phase>: <round-up-to-10 of p90>`; zero windowed events → `insufficient data: N events`, exit 0 (fail-open reporting, all errors exit 0 with reason).

## Verification (commands run + pass/fail)
- `go test ./tools/turn-report/ -count=1` — PASS (`ok ... 0.014s`, 8/8 contract tests).
- `go test ./tools/turn-report/ -count=1 -cover` — PASS, coverage 78.0% of statements (new implementation code).
- `go vet ./tools/turn-report/` — clean (VET_OK).
- `go build ./tools/turn-report/` — BUILD_OK.
- `gofmt -l tools/turn-report/` — clean.
- Placeholder grep (TODO/FIXME/placeholder) over main.go — 0 hits; file 195 lines, non-empty.

## Open edges for the next phase
- Unreadable events dir prints `insufficient data: 0 events (<err>)` and exits 0 by design (fail-open); a stricter mode could exit 2, but the parent AC pins fail-open.
- JSONLY day-file date matching is filename-based (events carry no timestamp field besides the file date) — matches the daemon EventSink day-file scheme.
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
——
