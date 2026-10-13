# feature handoff (feature-aizm)

## Gate-fix addendum (post-close sweep GREEN)
- internal/core/postclose_sweep.go: scan() was an inert RED stub → 4 pinned behavior tests failed the daemon gate (`expected bead reopened to open, got status ""`). Implemented the sweep contract: recently closed beads (≤24h, metadata test_command) re-verified via runCommand; fail→fail → reopen (StatusOpen) + `postclose-reopened` + comment carrying BOTH outputs (`runEvidence` falls back to the error string when the command fails silently — `false` case); fail→pass → `postclose-flake`, no reopen/comment; bead already carrying `postclose-reopened` → `needs-human`, no second reopen; Start = hourly ticker loop; all mutations fail-open.
- CHANGELOG.md: Added entry.
- Verification: `go test ./internal/core/ -run 'TestPostCloseSweep' -count=1 -v` — PASS 6/6 (was 4 FAIL at gate). `go vet ./internal/core/` clean, `gofmt -l` clean. Commit via git show --stat (2 files, +101/−6).
- Note: `FAIL src/x/a.spec.ts` line in the gate log is fixture output of a Prespawn test, not a real failing suite.

## What changed (files + one line each)
- cmd/worker/hindsight_sensor.go (new): hindsight extension-connectivity sensor — after each bead, verifies the hindsight pi extension entry point resolves when allowlisted for the phase (bootstrap-failed → reason `bootstrap-failed`) and that the session made ≥1 `hindsight_*` toolCall (reason `no-hindsight-tool-calls`); findings land as a loud `[HindsightSensor] FAIL-OPEN:` log line plus `degrade.RecordAttempt("hindsight-extension")` (success recorded on the healthy path); unobservable telemetry (no session jsonl) fails open silently; never gates the bead verdict.
- cmd/worker/main.go: `runHindsightSensor(config.Pi, HYDRA_HOME, bead)` wired after `emitSessionToolMixStats` in runWorker.
- cmd/worker/hindsight_sensor_test.go (new): 7 tests — pure verdict table (not-allowlisted/both resolved/telemetry-unreadable/zero-calls/healthy), hindsightToolCalls counting, degrade-record matrix via real session-jsonl + fake pi-package fixtures (bootstrap 1 attempt/0 success; healthy 2/1; zero-calls 1/0; not-allowlisted records nothing), resolveHindsightEntry sentinels (empty agent dir → `errHindsightNoAgentDir`; empty tree → `errHindsightUnresolved`; fake package → ext.ts), loud-log marker contract (`[HindsightSensor]` + subsystem name present in captured log).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- Duplicate-mechanism grep: no prior `hindsight` sensor/connectivity/event code in tree (only hindsight-recall/retain degrade counters for the HTTP pre-query, not the pi extension). PASS.
- `go build ./cmd/worker/` — PASS (OK).
- `go vet ./cmd/worker/` — PASS (no output).
- `gofmt -l` on both new files — clean.
- `go test ./cmd/worker/ -run 'TestHindsight|TestLoudLog|TestResolveHindsight' -count=1` — PASS (7 tests; 4 intermediate fixture failures fixed during the run, final green).
- Commit verified via `git show --stat HEAD` (4 files, +352).

## Open edges for the next phase
- Worker-process degrade counters remain process-local (documented open edge in internal/degrade): the `hindsight-extension` subsystem surfaces in worker logs now, in the Pan digest only once cross-process degrade reporting lands.
- Zero-hindsight-calls verdict can false-positive on beads whose agent legitimately skips recall (prompt says "skip silently"); sensor deliberately records this as fail-open signal, not error — tune threshold if churn observed.
- Sensor reads the same session jsonl twice (emitSessionToolMixStats + sensor); a shared stats gather would save a re-read if profiling ever shows it matters.
