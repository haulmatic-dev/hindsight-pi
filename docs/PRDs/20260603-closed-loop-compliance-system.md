# Hydra: Closed-Loop Compliance System

## Context

This document captures Hydra-specific issues discovered during the outreach-workflow campaign-centric ICP builder project, and the closed-loop compliance system built to address them.

**Related work in outreach-workflow (kept separate):**
- Campaign-centric ICP builder feature: `outreach-workflow/feature/icp-builder-skill`
- Compliance tools: `outreach-workflow/tools/hydra-compliance-analyzer/`, `outreach-workflow/tools/gap-to-prd/`

**This document lives in Hydra repo so Hydra improvements stay with Hydra.**

---

## Problems Discovered

### 1. Hydra Builds Functions, Not Systems

Hydra reliably builds individual FR beads (functions, types, tests). But when all FR beads close, integration gaps remain:

| Gap Type | Example from Campaign ICP Project |
|----------|-----------------------------------|
| Missing HTTP routes | `POST /campaigns/{name}/icps` declared in PRD ACs but never wired in `server.go` |
| Untested acceptance criteria | US-002 AC "user can activate campaign" had no corresponding test |
| Unconnected components | `search.go` built but no handler called it |
| Phantom files | Old flat-ICP files (`icp/*.yaml`) still existed outside PRD scope |

**Root cause:** Task-breakdown generates FR-scoped beads (infrastructure pieces) but not US-scoped integration beads (how pieces connect).

### 2. Compliance Gate Was Passive

The compliance gate in task-breakdown (`runner.mjs`):
- Ran 5 checks (exports, routes, ACs, placeholders, phantoms)
- **Reported** gaps in the gate bead description
- Did **not** create work to fix them

Result: Human had to read gate output, manually create fix beads, re-run. Defeats autonomous orchestration.

### 3. File Races with Concurrent Workers

With 3 concurrent workers:
- Worker A reads `manager.go` to add method
- Worker B overwrites `manager.go` with new version
- Worker A writes back stale version → **data loss**

We saw this when `manager.go` was deleted by a racing worker during campaign ICP development.

### 4. Worker Timeout + Auto-Close Issues

| Issue | Impact | Fix Applied |
|-------|--------|-------------|
| Pi subprocess hung with sporadic output | Worker waited forever, bead stuck `in_progress` | Added `maxTotalRuntime` (15m) to kill Pi even with output |
| Worker timed out, bead auto-closed | Retry loop re-opened same bead, same hang | Added `checkWorkComplete` guard — auto-close on timeout if files modified + tests pass |
| MiniMax API rate limit (429) | All workers failed, 337 beads auto-closed | Switched provider to kimi-coding |

### 5. Pagination Bug in Bead Discovery

`ListBeads()` fetched only first page (~100 beads). With 337+ beads, Hydra thought remaining beads didn't exist.

**Fix:** Added `--limit 500` pagination loop in `client.go` + `ParseBeadListPaginated()` in `parser.go`.

### 6. DAG Dead Dependencies

Archived parent beads left orphaned children with dead dependency links. Children never became ready.

**Fix:** Manually removed 5 dead dependency links. Need automated cleanup.

---

## Solution: Closed-Loop Compliance System

### Architecture

```
PRD → Task-Breakdown → Hydra Executor → All Beads Closed
                                              │
                                              ▼
                                    ┌─────────────────┐
                                    │ ComplianceCycle │ (background goroutine)
                                    │ Manager (poll)  │
                                    └────────┬────────┘
                                              │
                    ┌─────────────────────────┴─────────────────────────┐
                    │                                                   │
                    ▼ GAPS FOUND                                        ▼ ZERO GAPS
          ┌─────────────────┐                                 ┌─────────────────┐
          │ Run Analyzer    │                                 │  PRD APPROVED   │
          │ (5 checks →     │                                 │  Log + notify   │
          │  gap-report.json)│                                └─────────────────┘
          └────────┬────────┘
                   │
                   ▼
          ┌─────────────────┐
          │   gap-to-prd    │
          │ (mini-PRD .md)  │
          └────────┬────────┘
                   │
                   ▼
          ┌─────────────────┐
          │ pi skill task-  │ ◄── DRY: Reuse existing skill
          │ breakdown --    │     No duplication
          │ file req.json   │
          └────────┬────────┘
                   │
                   ▼
          [HYDRA CYCLE N+1]
          New gap beads picked up by scheduler tick
```

### Components Built

#### 1. ComplianceCycleManager (`internal/core/compliance_cycle.go`)

Background goroutine started in `Overseer.Start()`. Polls every 30s:

```go
type ComplianceCycleManager struct {
    config      ComplianceCycleConfig  // max_cycles, poll_interval, paths
    overseer    *Overseer
    cycles      int                      // current cycle counter
}

func (cm *ComplianceCycleManager) Start(ctx context.Context) {
    // Every poll_interval:
    // 1. Check all beads closed + no active workers
    // 2. Run analyzer → gap-report.json
    // 3. If gaps: run gap-to-prd → mini-PRD
    // 4. Invoke: pi skill task-breakdown --file req.json
    // 5. Hydra picks up new beads on next tick
    // 6. Repeat until zero gaps or max_cycles reached
}
```

**Config:**
```yaml
compliance_cycle:
  enabled: true           # default: false
  max_cycles: 5           # stop after N iterations
  poll_interval: 30s      # how often to check
  project_dir: "."
  prd_path: "docs/PRDs/feature.md"
```

#### 2. Analyzer Integration

Runs `hydra-compliance-analyzer` (lives in outreach-workflow, but reads any PRD):

```bash
hydra-compliance-analyzer \
  --prd docs/PRDs/feature.md \
  --project . \
  --output gaps/gap-report-cycle-N.json \
  --cycles N
```

Checks:
1. `verify-exports` — exports match PRD signatures
2. `verify-routes` — PRD routes wired in router
3. `verify-us-ac` — acceptance criteria have tests
4. `detect-placeholders` — no empty files
5. `detect-phantom-files` — no files outside PRD scope

#### 3. Gap-to-PRD Integration

Converts gap report to mini-PRD:

```bash
gap-to-prd \
  --input gaps/gap-report-cycle-N.json \
  --output gaps/gap-prd-cycle-N.md
```

Output format: valid PRD markdown with FRs, US, ACs, target files. Feed directly to task-breakdown.

#### 4. Task-Breakdown Invocation (Option B — DRY)

Instead of duplicating task-breakdown logic in Go, we shell out to existing Pi skill:

```go
args := []string{"skill", "task-breakdown", "--file", reqPath}
cmd := exec.Command("pi", args...)
```

**Why:** Task-breakdown is ~2000 lines of JavaScript with complex PRD parsing, bead generation, dependency analysis. Replicating it in Go would create a maintenance nightmare.

---

## Files Changed

### New Files

| File | Description |
|------|-------------|
| `internal/core/compliance_cycle.go` | ComplianceCycleManager implementation |
| `internal/core/compliance_cycle_test.go` | Unit tests (4 tests, all pass) |

### Modified Files

| File | Change |
|------|--------|
| `internal/core/config.go` | Added `ComplianceCycleConfig` to `Config` struct, default values, YAML parsing |
| `internal/core/overseer.go` | Added `complianceCycle *ComplianceCycleManager` field, initialize in `NewOverseer()`, start goroutine in `Start()` |

---

## Test Results

### Unit Tests

```bash
cd /opt/dev-tools/components/hydra
go test ./internal/core/... -run TestComplianceCycle -v
```

| Test | What | Status |
|------|------|--------|
| `areAllBeadsClosed/all_closed` | Empty project → all closed | ✅ PASS |
| `areAllBeadsClosed/one_open` | One open bead → not closed | ✅ PASS |
| `areAllBeadsClosed/all_open` | All open → not closed | ✅ PASS |
| `hasGaps/with_gaps` | Parse gap report with gaps | ✅ PASS |
| `hasGaps/without_gaps` | Parse gap report with zero gaps | ✅ PASS |
| `runGapToPRD` | Real gap-to-prd binary → mini-PRD | ✅ PASS |
| `cycleExhaustion` | Max cycles reached → stop | ✅ PASS |

### End-to-End Test

Created test PRD with deliberate gap (`GET /api/status` route missing):

1. Analyzer detected route gap ✅
2. Gap-to-PRD generated valid mini-PRD ✅
3. Mini-PRD contained fix instructions ✅

---

## Current Limitations

### 1. Step 5 (createGapBeads) Invokes Pi But Doesn't Wait

```go
// Current: shells out to pi skill task-breakdown
cmd := exec.Command("pi", args...)
cmd.CombinedOutput()
```

**Issue:** We don't verify that beads were actually created. If task-breakdown fails or creates zero beads, the cycle manager thinks it succeeded.

**Fix needed:** Parse task-breakdown output for created bead IDs, verify they exist via `ListBeads()`.

### 2. No Integration Beads in Task-Breakdown (Yet)

Task-breakdown already creates `INT-xxx` beads per User Story, but:
- They don't explicitly declare routes to wire
- They don't have `compliance-critical` label
- Pi sees them as "build integration layer" (vague) not "wire POST /x to handler Y" (specific)

**Fix needed:** Structured PRD format with `wires_to`, `consumes` fields (see Next Steps).

### 3. Phantom File Detection is Noisy

Running analyzer on any PRD flags ~95+ "phantom" files (pre-existing files not in PRD scope). This creates noise in gap reports.

**Fix needed:** Configurable phantom file ignore patterns, or scope phantom detection to files modified during current project.

### 4. Compliance Cycle Doesn't Know About Work In Progress

The cycle manager only checks "all beads closed." It doesn't distinguish:
- Project complete vs. project not started
- All beads closed because done vs. all beads closed because empty

**Fix needed:** Track project state (started/in_progress/complete) via bead count or explicit marker.

---

## Next Steps (Hydra Improvements)

### Phase 1: Structured PRD Format (2h)

Add machine-readable sections to PRD:

```yaml
---
exports:
  - name: NewCreateCampaignHandler
    signature: func(store Store) http.HandlerFunc
    file: internal/campaigns/handler.go
    wires_to:
      - file: internal/api/server.go
        route: POST /campaigns
        
interfaces:
  - name: Store
    package: internal/campaigns
    methods:
      - CreateCampaign(ctx, name) error
      
routes:
  - method: POST
    path: /campaigns
    handler: NewCreateCampaignHandler
    file: internal/api/server.go
---
```

**Impact:** Task-breakdown generates explicit "wire route X to handler Y" beads.

### Phase 2: Enhanced Task-Breakdown (4h)

Modify `runner.mjs` to:
1. Parse structured PRD front matter
2. Generate `INT-xxx-WIRE` beads with exact instructions
3. Add `compliance-critical` label to integration beads
4. Generate `ARCH-xxx` beads for static analysis checks

### Phase 3: Layered Verification (3h)

Add per-layer verification to worker prompts:

```
Layer 1 (FR): go test ./package/...
Layer 2 (INT): go test ./internal/api/...
Layer 3 (E2E): go test ./cmd/... -run TestFeature
```

### Phase 4: File Race Prevention (4h)

Replace advisory MCP reservations with Git-based locking:
1. Worker creates branch: `hydra/b/<bead-id>`
2. Worker commits changes to branch
3. Compliance cycle merges branches after verification
4. Conflicts detected at merge time, not write time

### Phase 5: Test Quality Verification (3h)

Add test review step:
- After test bead closes, run `go test -coverprofile`
- Check coverage > threshold for modified files
- Flag beads with 0% coverage or tautological tests

---

## Branches

| Repo | Branch | Status |
|------|--------|--------|
| Hydra | `feature/closed-loop-compliance` | 3 commits, pushed to origin |
| outreach-workflow | `feature/hydra-closed-loop-compliance` | 3 commits (tools + docs), pushed |

**Hydra PR:** https://github.com/haulmatic-dev/hydra/pull/new/feature/closed-loop-compliance

---

## Appendix: Fixes Applied During Campaign ICP Development

### Worker Timeout Fixes

```yaml
# hydra-config.yaml
worker_timeout: 15m       # was 5m
handoff_timeout: 10m      # was 6m
activity_timeout: 30m     # Pi subprocess timeout
max_total_runtime: 15m    # NEW: kill Pi even with sporadic output
```

### Worker Binary Fix

```bash
ln -s hydra-worker hydra/bin/worker
```

Worker spawner looked for `bin/worker` but binary was `bin/hydra-worker`.

### Worker Env Builder Fix

```go
// worker_env_builder.go: added fallback for API key name
if apiKeyName == "" {
    apiKeyName = "pi.standard-agent.llm"  // fallback
}
```

### Pagination Fix

```go
// client.go: loop through pages
for {
    out, err := runBeads("list", "--limit", "500", "--page", strconv.Itoa(page))
    // ...parse, append, check has_more
}
```

### Auto-Close Guard

```go
// worker/main.go: on timeout, check if work was actually done
func checkWorkComplete(projectPath string, bead types.Bead) bool {
    // Check if target files were modified
    // Check if tests pass
    // If yes, auto-close bead instead of retrying
}
```

---

## Contact

This document maintained by Hydra team. For outreach-workflow specific issues, see `outreach-workflow/docs/PRDs/20260603-hydra-closed-loop-compliance.md`.
