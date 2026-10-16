# bug handoff (3ocq memory-governor follow-up)

## What changed (files + one line each)
- internal/core/resource_monitor.go: absolute MemAvailable spawn floor — new `minMemAvailableMB` field + `readMemAvailableMB` probe (direct /proc/meminfo parse, MemFree+Buffers+Cached fallback, kB→MB) + `SetMinMemAvailableMB`/`SetMemAvailableProbe` seams; `CanSpawnWorker` refuses spawn below the floor (typed log "OOM guard"), fail-open on probe error, check skipped at <=0.
- internal/core/config.go: `ResourceMonitoringConfig.MinMemAvailableMB` (`resource_monitoring.min_memory_available_mb`, 0 = default 2500, negative disables) with OOM-motivation comment.
- internal/core/overseer.go: monitor construction applies the knob (zero → `defaultMinMemAvailableMB` = 2500) and logs the floor.
- internal/core/resource_monitor_memavailable_test.go (new): 5 tests — blocks below floor, allows above, fail-open on probe error, disabled at 0/-1 (nil probe proves skip), real /proc/meminfo parse sanity.
- CHANGELOG.md, hydra-config.yaml.example, docs/operations/host-setup.md: knob documentation.

## Verify-first / A1–A3
- A1 defect state: the cgroup half of the fix (per-worker `memory.max` + V8 heap caps) ALREADY LANDED — commit 6e20cd6a (`hydra-3ocq: per-worker cgroup memory containment + V8 heap caps (#305)`), ancestor of HEAD; verified `git merge-base --is-ancestor` YES. The remaining gap was the MemAvailable-before-spawn refusal: only a percentage gate existed (`max_memory_percent`, ignores free headroom), no absolute floor (`rg MinMemAvailable|MemAvailableMB` → 0 hits).
- A3 duplicate check: no existing MemAvailable-floor mechanism; the landed cgroup containment covers the runaway-single-worker half, this bead adds the spawn-refusal half.
- AIMD governor itself (`concurrency_governor.go`) still scales only on outcomes — deliberately untouched; the floor at the single scheduler spawn chokepoint (scheduler.go `CanSpawnWorker` gate) is the smallest change that stops OOM-spawn storms.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run 'TestResourceMonitor|TestReadMemAvailableMB' -count=1` — PASS (ok; 5 new + existing).
- `gofmt -l` on touched files — clean (pre-existing gofmt drift at HEAD in config.go OverseerConfig alignment verified NOT introduced by this bead: `git show HEAD:internal/core/config.go | gofmt -l` also flags it; left untouched per smallest-change).
- Commit verified via `git show --stat HEAD` (7 files, +148/−3).

## Open edges for the next phase
- AIMD governor has no memory signal (limit changes only on worker outcomes); if wanted, feed floor-proximity into a `governor` scale-down — separate bead.
- Default floor 2500MB applies when the knob is absent; an explicit negative is the only disable path. Very low-memory CI hosts relying on DefaultConfig may block spawns when MemAvailable < 2.5G (intended fail-closed; probe errors still fail open).
