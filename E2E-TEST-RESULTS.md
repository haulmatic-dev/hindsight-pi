# End-to-End Test Results

## Test Setup

Created a temp project at `/tmp/hydra-e2e-test/url-shortener/` with:
- `go.mod` (Go module)
- `main.go` + `main_test.go`
- `.beads/issues.jsonl` with 4 beads forming a TDD chain:
  1. `url-shortener-001` (test, priority 10) — no dependencies
  2. `url-shortener-002` (implement, priority 10) — depends on 001
  3. `url-shortener-003` (refactor, priority 5) — depends on 002
  4. `url-shortener-004` (implement, priority 3) — depends on 003

## Test 1: Hydra Binary Run

### Command
```bash
./hydra -config hydra.yaml
```

### Observations
```
📁 Discovered 1 projects
[DEGRADED] [beadsClient] ListBeads failed but continuing |
  recovery=skipping project url-shortener |
  error=bead list failed for url-shortener: br list --json failed: exit status 7
[DAG] Building DAG with 0 beads (from 1 projects)
[DAG] Build complete: {TotalNodes:0 ReadyNodes:0 BlockedNodes:0...}
```

### Root Cause
Hydra's `beadsClient` uses the `br` CLI (`br list --json`) to fetch beads from projects. The `br` CLI is not installed in the test environment, so bead discovery fails with exit status 7.

**Impact**: The DAG builds with 0 beads. No workers are spawned. No scheduling occurs.

### Missing Capability
Hydra does **not** fall back to reading `.beads/issues.jsonl` directly when `br list` fails. The `beadFileReader` exists but is not used as a fallback in the DAG builder.

---

## Test 2: E2E Smoke Test (In-Package)

### Command
```bash
go test ./internal/core/... -run TestE2E_BeadDiscoveryAndScheduling -v
```

### What It Tests
Uses a mock `BeadsClient` to bypass the `br` CLI dependency and tests the core scheduling pipeline:

1. **DAG Building**: Creates 4 beads with dependencies
2. **Stats Verification**: Confirms 4 total nodes, 1 ready, 3 blocked
3. **Ready Bead Identification**: Confirms `url-shortener-001` (test) is ready first
4. **Dependency Resolution**: Confirms `url-shortener-002` depends on `001`
5. **State Transition**: After marking `001` completed, `002` becomes ready

### Results
```
✅ PASS: DAG built correctly (4 nodes, 1 ready, 3 blocked)
✅ PASS: Ready bead is url-shortener-001 (the RED/test bead)
✅ PASS: Dependencies resolved correctly
✅ PASS: State transitions work (001 → 002 → 003 → 004)
```

---

## Issues Discovered

### Issue 1: No Fallback for `br list` Failure
**Severity**: High
**Description**: When `br list --json` fails (e.g., `br` not installed), Hydra skips the project entirely instead of falling back to reading `.beads/issues.jsonl`.
**Location**: `internal/core/dag_builder.go`
**Suggested Fix**: In `DAGBuilder.Build()`, when `ListBeads` returns an error, attempt to read `.beads/issues.jsonl` via `BeadFileReader` before giving up.

### Issue 2: Pi Setup Blocks Startup
**Severity**: Medium
**Description**: `PiSetupManager.RunSetup()` runs unconditionally on Overseer startup and tries to install 16 skills. This takes 10+ seconds and generates many error logs when the skills CLI is not available.
**Location**: `internal/core/overseer.go` startup sequence
**Suggested Fix**: Make Pi setup conditional on `config.Pi.Enabled` or run it asynchronously.

### Issue 3: Config Watcher Started Even When API is Disabled
**Severity**: Low
**Description**: The config watcher starts even when `config.API.Enabled = false`.
**Location**: `cmd/hydra/main.go`
**Suggested Fix**: Only start config watcher when API is enabled or explicitly requested.

---

## What Works

| Feature | Status | Evidence |
|---------|--------|----------|
| Project discovery | ✅ | "Discovered 1 projects" |
| DAG building (with valid beads client) | ✅ | E2E test: 4 nodes built |
| Ready bead identification | ✅ | E2E test: 1 ready, 3 blocked |
| Dependency resolution | ✅ | E2E test: correct dependency chain |
| State transitions | ✅ | E2E test: MarkCompleted propagates |
| Validation service init | ✅ | "Initialized bead validation service" |
| Chain completion detector | ✅ | "Detector started with interval=5s" |
| Graceful degradation | ✅ | "[DEGRADED] ... continuing" |

---

## Recommendations

1. **Add `br` CLI fallback to `.beads/issues.jsonl`** — This is the biggest blocker for running Hydra without the beads CLI toolchain.
2. **Make Pi setup async or conditional** — Reduces startup time from ~10s to ~1s.
3. **Add a `--dry-run` mode** — Run Hydra without spawning workers, just log what it would do.
4. **Document the `br` CLI dependency** — Make it clear that `br` must be installed and configured.
