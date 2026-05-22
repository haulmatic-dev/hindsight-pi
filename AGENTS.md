# Hydra Agent Configuration

This document contains important information for agents working on the Hydra project.

## Recent Fixes and Improvements

### 1. Bead Closure and Dependency Handling (March 2026)

**Problem**: Beads were being processed by workers but not properly closed due to dependency issues.

**Root Causes**:
- `br close` returns exit code 0 even when skipping beads due to dependencies
- DAG had stale dependency data
- Status constants didn't include "closed" (only "completed")

**Solutions Implemented**:

#### a) Fixed CloseBead Function (`internal/beads/client.go`)
- Parse `br close` output for "Warning: Skipped" and "blocked by" messages
- Verify bead status after close using `GetBead()`
- Return error if bead not actually closed
- Updated to use `-r` flag instead of `-m` for reason

```go
// CloseBead now:
// 1. Parses output for dependency warnings
// 2. Verifies bead status after close
// 3. Returns error if not actually closed
```

#### b) Added StatusClosed Constant (`pkg/types/bead.go`)
- Added `StatusClosed = "closed"` constant
- Updated `IsTerminal()` to include `StatusClosed`
- Beads CLI uses "closed" status, not "completed"

#### c) Switched from DAG to br ready (`internal/core/overseer.go`)
- Replaced `getReadyBeadsFromDAG()` with `getReadyBeads()`
- Uses `beadsClient.GetReadyBeads()` → calls `br ready --json`
- More reliable as it queries actual bead status
- Better filtering for running/completed beads

#### d) Updated Overseer Error Handling
- Detects close failures due to dependencies
- Marks worker as failed when close fails
- Triggers retry scheduling

### 2. Config Parsing Fix (March 2026)

**Problem**: `LoadConfig()` just returned `DefaultConfig()` without parsing YAML file.

**Solution**: Implemented proper YAML parsing with support for:
- `projects: []` array format (old)
- `projects: {auto_discover: false, list: []}` object format (new)

### 3. Worker Configuration Fix (March 2026)

**Problem**: Workers only received basic info (ID, Project, Repo, Type) via environment variables.

**Solution**: 
- Added `HYDRA_BEAD_TITLE` and `HYDRA_BEAD_DESCRIPTION` environment variables
- Updated WorkerConfig struct to include these fields
- Worker now populates bead object with complete information for agent prompts

### 4. Project Path Resolution (March 2026)

**Problem**: `closeWorkerBead` couldn't find project paths for beads from unconfigured projects.

**Solution**:
- Added `ProjectPath` field to Worker struct
- Save projectPath in worker record during spawn
- Use worker.ProjectPath as primary source in closeWorkerBead
- Fallback to searching configured projects

## Testing

### Unit Tests Added

1. **Config Tests** (`internal/core/config_test.go`):
   - `TestLoadConfig_ValidYAML` - Valid YAML parsing
   - `TestLoadConfig_ArrayFormat` - Array format projects
   - `TestLoadConfig_ObjectWithListFormat` - Object with list format
   - `TestLoadConfig_DefaultValues` - Default value preservation
   - `TestLoadConfig_FileNotFound` - Error handling
   - `TestLoadConfig_InvalidYAML` - Invalid YAML handling

2. **Beads Client Tests** (`internal/beads/client_test.go`):
   - `TestCloseBead_Success` - Successful closure
   - `TestCloseBead_CLIError` - CLI error handling
   - `TestCloseBead_DetectsSkippedDueToDependencies` - Dependency detection
   - `TestCloseBead_DetectsBlockedByDependencies` - Blocked detection
   - `TestCloseBead_DifferentReasons` - Various close reasons

### Running Tests

```bash
# Run all tests
go test ./...

# Run specific packages
go test ./internal/beads/... -v
go test ./internal/core/... -v

# Run specific tests
go test -run TestCloseBead
go test -run TestLoadConfig
```

## Performance Optimization

Based on testing with 122 beads:

**Optimal Configuration**:
```yaml
overseer:
  poll_interval: 5s
  max_workers: 5
  worker_timeout: 3m
  max_retries: 2
  retry_delay: 5s
```

**Results**:
- 30-minute test: 89 beads closed (73% success rate)
- Processing rate: ~3 beads per minute
- Worker duration: 40-120 seconds per bead

## Troubleshooting Guide

### Beads Not Closing

**Symptoms**: Workers complete but beads remain open.

**Check**:
1. Dependency status: `br show <bead-id>`
2. Logs for warnings: `grep "blocked by\|dependencies" /tmp/hydra.log`
3. Ensure root beads processed first

### Workers Failing

**Symptoms**: Workers exit with error code.

**Check**:
1. Worker logs: `grep "Worker.*failed" /tmp/hydra.log`
2. Pi agent works: verify `pi` binary is in PATH and `HYDRA_PI_ENABLED=true` is set
3. Project path in hydra.yaml

### Config Not Loading

**Symptoms**: Hydra uses default config instead of hydra.yaml.

**Check**:
1. File exists: `ls -la /path/to/hydra.yaml`
2. Valid YAML: `cat /path/to/hydra.yaml | head -20`
3. Projects section properly formatted

## Common Commands

```bash
# Build binaries
go build -o bin/hydra ./cmd/hydra
go build -o bin/worker ./cmd/worker

# Run hydra
./bin/hydra -config /path/to/hydra.yaml

# Check bead status
cd /project/path && br list
cd /project/path && br ready
cd /project/path && br show <bead-id>

# View logs
tail -f /tmp/hydra.log | grep -E "(Worker|Completed|Closed)"
```

## Dependencies

- **pi**: Pi coding agent must be installed and in PATH
- **br**: Beads CLI must be installed
- **bv**: Beads viewer for triage
- **Go 1.23+**: For building

## Code Structure

```
cmd/
  hydra/        # Main hydra binary
  worker/       # Worker binary

internal/
  beads/        # Beads client
  core/         # Overseer and config
  intelligence/ # Intelligence services

pkg/
  types/        # Type definitions

docs/
  MONITORING.md # Monitoring documentation
```

## Future Improvements

- [ ] Prometheus metrics endpoint
- [ ] Grafana dashboard
- [ ] Better dependency visualization
- [ ] Worker pool optimization
- [ ] Retry backoff strategies

---

*Last updated: March 2026*
