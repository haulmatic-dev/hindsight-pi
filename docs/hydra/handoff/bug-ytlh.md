# bug handoff (ytlh)

## What changed (files + one line each)
- internal/core/memory_pressure.go (new): memory-throttle probe — `parsePSIAvg10`, `parseMemBytes` ("max" → 0), `memoryThrottlePressure(psi, cur, high)` (PSI "some" avg10; 100 override when memory.current >= memory.high; missing files fail-open to 0); `ConcurrencyGovernor.MaybeScaleDownForMemoryPressure()` — halve limit (floor 1), arm lastFailure + lastChange so MaybeRecover cannot climb back while throttled, emit `aimd_scale_down` reason `memory_throttle`; `SetPressurePaths` test seam; default probe paths `/sys/fs/cgroup/system.slice/hydra.service/{memory.pressure,memory.current,memory.high}`.
- internal/core/concurrency_governor.go: `GovernorConfig.MemoryPressureThreshold` (yaml `memory_pressure_threshold`; 0 → default 50, negative disables), governor fields `memThreshold`/`psiPath`/`curPath`/`highPath`, `memory_throttle` case added to the aimd_scale_down event switch.
- internal/core/scheduler.go: `Tick()` calls `o.concurrencyGovernor.MaybeScaleDownForMemoryPressure()` right after `MaybeRecover()` (fail-open).
- docs/architecture/shepherd.md §4: memory throttle feedback documented (signals, threshold, fail-open, disable knob).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- A1 reproduction on base: `rg -i "memory.pressure|memory.high|PSI" internal/ --glob '!*_test.go'` → 0 hits; governor feedback surface is only `RecordOutcome`/`MaybeRecover` (outcome-driven) — no memory signal existed. Live host has the probe target: `/sys/fs/cgroup/system.slice/hydra.service/memory.pressure` present (VERIFIED, read directly). Live probe test against real paths: pressure 0.22% (idle, fail-open no-op) — the mechanism that was blind during the 2026-10-15 regime (PSI would read high / current ≥ high there).
- Narrow suite: `go test ./internal/core/ -run 'TestParsePSIAvg10|TestParseMemBytes|TestMemoryThrottlePressure|TestMaybeScaleDownForMemoryPressure|TestMemoryThrottle' -count=1 -v` — 9/9 PASS (halving, below-threshold noop, negative-disable, floor-at-1, blocks-recovery-then-recovers, aimd_scale_down event, PSI/mem parsers, at-high override).
- Regression: `go test ./internal/core/ -run 'TestScheduler|TestGovernor' -count=1` — 21 PASS.
- `go build ./internal/core/` — PASS. `go vet ./internal/core/` — clean. `gofmt -l` on touched files — clean.
- Commit verified via `git show --stat HEAD` (6 files, +374/−1).

## AC mapping
- AIMD inputs include PSI memory.pressure avg10 or memory.current vs memory.high ratio → VERIFIED (memoryThrottlePressure covers both signals; scheduler-tick wired).
- Scale down on throttle, not just on death → VERIFIED (TestMaybeScaleDownForMemoryPressure_HalvesLimit; recovery blocked while throttled — TestMemoryThrottle_BlocksRecoveryThenRecovers).
- max_workers unheld until operator intervention → VERIFIED (limit halves automatically at threshold; recovers +1 per recovery interval once pressure clears).

## Open edges for the next phase
- Threshold 50% PSI avg10 is a default, not a tuned value; under real throttle regime observe actual PSI numbers and tune via `memory_pressure_threshold`.
- Probe is the daemon's own service slice; workers confined to child cgroups under the same slice, so slice-level PSI covers them; a per-worker probe would be finer-grained but costs a file read per worker.
- Only the scheduler tick path is wired; if any spawn path bypasses Tick for long stretches, throttle detection is delayed to the next tick.
