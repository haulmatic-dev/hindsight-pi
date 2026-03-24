# Product Requirements Document: Epic Support for Hydra

## 1. Executive Summary

Hydra currently processes all bead types equally, including organizational epics. This PRD adds support for:
1. **Epic Filtering**: Skip processing beads with `type="epic"` (they're organizational containers, not actionable tasks)
2. **Enhanced Worker Context**: Pass full bead JSON to workers for better context
3. **Epic Auto-Close**: Periodically scan and close epics when all their child tasks complete

**Business Value**: Keeps project organization clean by auto-closing phase epics when work completes, without manual intervention.

## 2. User Stories

**As a** project manager  
**I want** epics to auto-close when all their tasks are done  
**So that** I don't have to manually close them to keep the project status clean

**As a** Hydra operator  
**I want** workers to receive complete bead context  
**So that** they have all necessary information (labels, dependencies, metadata) to process tasks correctly

**As a** system administrator  
**I want** epic processing to be skipped  
**So that** Hydra doesn't waste resources trying to execute organizational containers

## 3. Functional Requirements

### FR-001: Epic Filtering in Overseer
Prevent workers from spawning for epic-type beads.

**Target:** `internal/core/overseer.go:shouldSpawnWorker()`

**Acceptance Criteria:**
- Add epic type check in `shouldSpawnWorker()` function around line 2772
- Skip spawning workers for beads with `Type == "epic"`
- Log message: "⏸️  Skipping epic (organizational): <bead-id>"

### FR-002: Enhanced Worker Context via JSON
Pass complete bead data to workers instead of minimal environment variables.

**Target:** `internal/core/overseer.go:spawnWorkerInternal()`, `cmd/worker/main.go:parseConfig()`

**Acceptance Criteria:**
- Serialize full bead object to JSON in `spawnWorkerInternal()`
- Pass via `HYDRA_BEAD_JSON` environment variable
- Worker deserializes complete bead with all fields (labels, dependencies, metadata, etc.)

### FR-003: Epic Auto-Close Scanner
Periodically scan open epics and close them when all child tasks are complete.

**Target:** `internal/core/epic_scanner.go` (new), `internal/core/overseer.go:Start()/Stop()`

**Acceptance Criteria:**
- Create background scanner goroutine in overseer
- Run every 5 minutes (configurable via `internal/core/config.go`)
- Map req-XXX labels to epic IDs (req-001-020 → Phase 0, etc.)
- Query open tasks for each epic's req-XXX range
- Close epic with message "all dependencies resolved" when no open tasks remain
- Scanner runs async with timeout (no blocking of overseer loop)
- Errors are logged but don't crash overseer (resilient to failures)

### FR-004: Req-XXX to Epic Mapping
Define the relationship between requirement IDs and phase epics.

**Target:** `internal/core/epic_scanner.go:getEpicForReq()`

**Acceptance Criteria:**
- Phase 0 (hydra-31h): req-001 to req-020
- Phase 1 (hydra-2qk): req-021 to req-040
- Phase 2 (hydra-33k): req-041 to req-060
- Phase 3 (hydra-itm): req-061 to req-079

## 4. Technical Considerations

### Architecture
- **Epic Filtering**: Synchronous check in `shouldSpawnWorker()`
- **Worker Context**: JSON serialization/deserialization
- **Epic Scanner**: Background goroutine with ticker

### Files to Modify
- `internal/core/overseer.go` - Epic filter, scanner integration
- `cmd/worker/main.go` - Parse HYDRA_BEAD_JSON

### Files to Create
- `internal/core/epic_scanner.go` - Scanner implementation

### Req-XXX Mapping Logic
```go
func getEpicForReq(reqNum int) string {
    switch {
    case reqNum >= 1 && reqNum <= 20:
        return "hydra-31h"  // Phase 0
    case reqNum >= 21 && reqNum <= 40:
        return "hydra-2qk"  // Phase 1
    case reqNum >= 41 && reqNum <= 60:
        return "hydra-33k"  // Phase 2
    case reqNum >= 61 && reqNum <= 79:
        return "hydra-itm"  // Phase 3
    default:
        return ""
    }
}
```

## 5. Implementation Mapping

### Functional Requirements

| Requirement | Target File | Function/Location |
|-------------|-------------|-------------------|
| FR-001: Epic Filtering | `internal/core/overseer.go` | `shouldSpawnWorker()` around line 2772 |
| FR-002: Enhanced Worker Context | `internal/core/overseer.go`, `cmd/worker/main.go` | `spawnWorkerInternal()` in overseer, `parseConfig()` and bead initialization in worker |
| FR-003: Epic Auto-Close Scanner | `internal/core/epic_scanner.go` (new), `internal/core/overseer.go` | Scanner struct in new file, `Start()`/`Stop()` integration in overseer |
| FR-004: Req-XXX to Epic Mapping | `internal/core/epic_scanner.go` | `getEpicForReq()` helper function |

## 6. Risks and Mitigation

| Risk | Impact | Mitigation |
|------|--------|------------|
| Scanner queries slow down system | Medium | Run async, add timeout, make interval configurable |
| Worker fails to parse JSON | Low | Add error handling, fallback to env vars temporarily |
| Epic closes prematurely | Low | Verify all tasks in range are terminal (closed/failed) |

## 7. MoSCoW Prioritization

### Must Have
- Epic filtering in overseer
- Enhanced worker context (full bead JSON)
- Epic auto-close scanner

### Should Have
- Configurable scan interval
- Metrics for epic closures

### Could Have
- Master epic auto-close (when all phases complete)
- Epic progress dashboard

### Won't Have
- Manual epic close from worker
- Real-time epic notifications
- Epic reopen logic

## 8. Success Metrics

- **Epic Filtering**: 100% of epics skipped (no workers spawned)
- **Worker Context**: Workers receive complete bead data including labels
- **Epic Auto-Close**: Epics close within 5 minutes of last task completion
- **Performance**: No measurable impact on task throughput

## 9. Implementation Notes

Based on codebase analysis:

**Current State:**
- Epics have `type="epic"` in bead structure
- Tasks have `req-XXX` labels (req-001 to req-079)
- Worker receives minimal env vars (HYDRA_BEAD_ID, HYDRA_BEAD_TITLE, etc.)
- Overseer spawns workers via `spawnWorkerInternal()`
- `shouldSpawnWorker()` already filters approval-gate and approval types

**Integration Points:**
- Add epic check alongside existing filters in `shouldSpawnWorker()`
- Start scanner in `Overseer.Start()`
- Stop scanner in `Overseer.Stop()`
- Use existing `beadsClient` for queries

**No Backward Compatibility Required:**
- Changes are additive
- Existing functionality preserved
- New fields (HYDRA_BEAD_JSON) are optional for workers
