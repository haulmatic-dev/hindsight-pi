# External Tools Integration PRD

**Version:** 1.0  
**Date:** 2026-03-08  
**Status:** Draft  
**Author:** buddhi

---

## 1. Executive Summary

This PRD defines the integration of 4 external command-line tools into Hydra's orchestration layer. These tools enhance Hydra's capabilities without compromising ownership or flexibility.

### Integrated Tools

| Tool | Repository | Purpose |
|------|------------|---------|
| **BV** | Dicklesworthstone/beads_viewer | Graph analytics, PageRank, ready bead queries |
| **Process Triage (PT)** | Dicklesworthstone/process_triage | Intelligent process termination for hung workers |
| **UBS** | Dicklesworthstone/ultimate_bug_scanner | Pattern-based bug detection (1000+ rules) |
| **CASS** | Dicklesworthstone/cass | Session history search for context retrieval |

### Integration Philosophy
- **Shell-based**: All tools invoked via CLI (no library dependencies)
- **Read-only consumption**: Hydra queries tools, doesn't modify them
- **Graceful degradation**: Continue operating if tools are unavailable
- **Atomic bead compatible**: All integrations designed for 20-min atomic beads

---

## 2. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    Hydra Orchestrator (Go)                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Integration Layer (internal/tools)          │   │
│  │                                                          │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │   │
│  │  │ BVClient │ │ PTClient │ │ UBSClient│ │CASSClient│   │   │
│  │  │(shell)   │ │(shell)   │ │(shell)   │ │(shell)   │   │   │
│  │  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘   │   │
│  │       │            │            │            │         │   │
│  │       └────────────┴────────────┴────────────┘         │   │
│  │                          │                              │   │
│  │                   Tool Interfaces                       │   │
│  │                   (Go structs + JSON)                   │   │
│  └──────────────────────────┬──────────────────────────────┘   │
│                             │                                    │
├─────────────────────────────┼────────────────────────────────────┤
│                             │                                    │
│  ┌──────────────────────────▼──────────────────────────────┐   │
│  │              Core Components (Hydra-owned)               │   │
│  │                                                          │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐   │   │
│  │  │   Overseer   │  │     DAG      │  │   Workers    │   │   │
│  │  │   (custom)   │  │   (custom)   │  │ (ephemeral)  │   │   │
│  │  └──────────────┘  └──────────────┘  └──────────────┘   │   │
│  │                                                          │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## 3. Tool Specifications

### 3.1 BV (Beads Viewer)

**Purpose:** Graph analytics, PageRank prioritization, ready bead queries

**Installation:**
```bash
# Installed via Agent Flywheel or manual install
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/beads_viewer/main/install.sh | bash
```

**Commands:**
```bash
# Get ready beads with priorities
bv --robot-triage --format json --project <path>

# Get parallel execution tracks
bv --robot-plan --format json --project <path>

# Get graph insights
bv --robot-insights --format json --project <path>

# Get next best bead
bv --robot-next --format json --project <path>
```

**Go Interface:**
```go
// internal/tools/bv/client.go
type Client struct {
    projectPath string
}

type TriageResult struct {
    ReadyBeads    []ReadyBead    `json:"ready_beads"`
    BlockedBeads  []BlockedBead  `json:"blocked_beads"`
    Metrics       TriageMetrics  `json:"metrics"`
}

type ReadyBead struct {
    ID          string  `json:"id"`
    Title       string  `json:"title"`
    Priority    int     `json:"priority"`
    PageRank    float64 `json:"page_rank"`
    Blockers    int     `json:"blockers"`
}

func (c *Client) GetReadyBeads() ([]ReadyBead, error)
func (c *Client) GetExecutionPlan() (*ExecutionPlan, error)
func (c *Client) GetInsights() (*Insights, error)
```

**Integration Points:**
- **Overseer polling (5s):** Query BV for ready beads sorted by PageRank
- **Scheduling decision:** Use BV's prioritization instead of custom sorting
- **Insights:** Display critical path, bottlenecks in REST API

**Error Handling:**
- If BV unavailable: Fall back to `br ready` with basic sorting
- Log warning, continue operation
- Metric: `bv_query_errors_total`

---

### 3.2 Process Triage (PT)

**Purpose:** Kill hung workers, intelligent process cleanup

**Installation:**
```bash
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/process_triage/main/install.sh | bash
```

**Commands:**
```bash
# Find stale processes (older than X minutes)
pt --robot --stale 20m --format json

# Kill specific process
pt --kill <pid>

# List all hydra workers
pt --filter "hydra-worker" --format json
```

**Go Interface:**
```go
// internal/tools/pt/client.go
type Client struct {
    staleThreshold time.Duration // 20m for atomic beads
}

type StaleProcess struct {
    PID         int       `json:"pid"`
    Command     string    `json:"command"`
    Runtime     string    `json:"runtime"`
    Confidence  float64   `json:"confidence"` // Bayesian score
    BeadID      string    `json:"bead_id,omitempty"`
}

func (c *Client) FindStaleProcesses() ([]StaleProcess, error)
func (c *Client) KillProcess(pid int) error
func (c *Client) FindHydraWorkers() ([]Process, error)
```

**Integration Points:**
- **Health monitor (30s):** Query PT for stale hydra-worker processes
- **Auto-kill:** Terminate workers exceeding 20m runtime
- **Bead status update:** Mark killed workers as FAILED

**Configuration:**
```yaml
# hydra.yaml
process_triage:
  enabled: true
  check_interval: 30s
  stale_threshold: 20m
  auto_kill: true
  confidence_threshold: 0.8
```

**Error Handling:**
- If PT unavailable: Use basic `ps` + `kill` fallback
- Log worker kills with context (bead ID, runtime)

---

### 3.3 UBS (Ultimate Bug Scanner)

**Purpose:** Pattern-based bug detection (1000+ rules) as pre-completion gate

**Installation:**
```bash
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/ultimate_bug_scanner/main/install.sh | bash
```

**Commands:**
```bash
# Scan changed files
ubs --json <file1> <file2> ...

# Scan directory
ubs --json --dir <path>

# Scan with severity filter
ubs --json --min-severity warning <files>
```

**Go Interface:**
```go
// internal/tools/ubs/client.go
type Client struct {
    minSeverity Severity // default: warning
}

type Severity string
const (
    SeverityInfo     Severity = "info"
    SeverityWarning  Severity = "warning"
    SeverityError    Severity = "error"
    SeverityCritical Severity = "critical"
)

type BugReport struct {
    Rule        string   `json:"rule"`
    Severity    Severity `json:"severity"`
    File        string   `json:"file"`
    Line        int      `json:"line"`
    Column      int      `json:"column"`
    Message     string   `json:"message"`
    Category    string   `json:"category"`
}

type ScanResult struct {
    FilesScanned int         `json:"files_scanned"`
    BugsFound    int         `json:"bugs_found"`
    Bugs         []BugReport `json:"bugs"`
}

func (c *Client) ScanFiles(files []string) (*ScanResult, error)
func (c *Client) ScanDirectory(dir string) (*ScanResult, error)
func (c *Client) HasCriticalBugs(result *ScanResult) bool
```

**Integration Points:**
- **Worker completion (pre-mark):** Run UBS scan on all changed files
- **Quality gate:** If bugs found → mark bead FAILED, report bugs
- **Metrics:** Track bug categories, common issues

**Workflow:**
```
Worker completes task
    ↓
Get changed files (via git diff)
    ↓
UBS scan --json <changed-files>
    ↓
If bugs found:
    - Log bugs
    - Mark bead FAILED
    - Return bugs in worker output
Else:
    - Mark bead COMPLETED
```

**Configuration:**
```yaml
ubs:
  enabled: true
  min_severity: warning
  fail_on_any_bug: true  # Strict mode
  exclude_patterns:
    - "*.generated.go"
    - "*_test.go"
```

**Error Handling:**
- If UBS unavailable: Log warning, skip scan (configurable)
- Partial scan failure: Report scanned files, log error

---

### 3.4 CASS (Coding Agent Session Search)

**Purpose:** Search session history for context and similar past tasks

**Installation:**
```bash
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/cass/main/install.sh | bash
```

**Commands:**
```bash
# Search sessions
cass search "<query>" --format json --limit 5

# Get session by ID
cass get <session-id> --format json

# List recent sessions
cass list --format json --limit 10
```

**Go Interface:**
```go
// internal/tools/cass/client.go
type Client struct {
    defaultLimit int
}

type Session struct {
    ID          string    `json:"id"`
    BeadID      string    `json:"bead_id"`
    StartTime   time.Time `json:"start_time"`
    EndTime     time.Time `json:"end_time"`
    Duration    string    `json:"duration"`
    Status      string    `json:"status"`
    Summary     string    `json:"summary"`
    FilesWorked []string  `json:"files_worked"`
}

type SearchResult struct {
    Query        string    `json:"query"`
    TotalMatches int       `json:"total_matches"`
    Sessions     []Session `json:"sessions"`
}

func (c *Client) Search(query string, limit int) (*SearchResult, error)
func (c *Client) GetSession(sessionID string) (*Session, error)
func (c *Client) ListRecent(limit int) ([]Session, error)
```

**Integration Points:**
- **Worker startup:** Search for similar past tasks
- **Context enrichment:** Pass relevant sessions to worker via env vars
- **Debugging:** Find similar failed attempts

**Workflow:**
```
Worker spawns
    ↓
cass search "<bead.title>" --limit 3
    ↓
If similar sessions found:
    - Add to worker context
    - Include successful patterns
    - Include failure warnings
    ↓
Worker executes with enriched context
```

**Configuration:**
```yaml
cass:
  enabled: true
  search_limit: 5
  min_similarity: 0.7
```

**Error Handling:**
- If CASS unavailable: Worker starts without context enrichment
- Log warning, continue operation

---

## 4. Integration Implementation

### 4.1 Directory Structure

```
internal/
└── tools/
    ├── bv/
    │   ├── client.go      # BV client implementation
    │   ├── types.go       # BV response types
    │   └── client_test.go # Unit tests
    ├── pt/
    │   ├── client.go
    │   ├── types.go
    │   └── client_test.go
    ├── ubs/
    │   ├── client.go
    │   ├── types.go
    │   └── client_test.go
    ├── cass/
    │   ├── client.go
    │   ├── types.go
    │   └── client_test.go
    └── registry.go        # Tool availability checks
```

### 4.2 Common Client Pattern

All tool clients follow this pattern:

```go
// internal/tools/bv/client.go (example)
type Client struct {
    projectPath string
    timeout     time.Duration
}

func NewClient(projectPath string) *Client {
    return &Client{
        projectPath: projectPath,
        timeout:     5 * time.Second,
    }
}

func (c *Client) exec(args ...string) ([]byte, error) {
    ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
    defer cancel()
    
    cmd := exec.CommandContext(ctx, "bv", args...)
    cmd.Dir = c.projectPath
    
    output, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("bv command failed: %w (output: %s)", err, output)
    }
    
    return output, nil
}

func (c *Client) GetReadyBeads() ([]ReadyBead, error) {
    output, err := c.exec("--robot-triage", "--format", "json")
    if err != nil {
        return nil, err
    }
    
    var result TriageResult
    if err := json.Unmarshal(output, &result); err != nil {
        return nil, fmt.Errorf("failed to parse BV output: %w", err)
    }
    
    return result.ReadyBeads, nil
}
```

### 4.3 Tool Registry

```go
// internal/tools/registry.go
type Registry struct {
    tools map[string]ToolStatus
}

type ToolStatus struct {
    Name      string
    Available bool
    Version   string
    Error     error
}

func (r *Registry) CheckAll() {
    r.tools["bv"] = r.checkTool("bv", "--version")
    r.tools["pt"] = r.checkTool("pt", "--version")
    r.tools["ubs"] = r.checkTool("ubs", "--version")
    r.tools["cass"] = r.checkTool("cass", "--version")
}

func (r *Registry) IsAvailable(tool string) bool {
    if status, ok := r.tools[tool]; ok {
        return status.Available
    }
    return false
}
```

### 4.4 Configuration

```yaml
# hydra.yaml
external_tools:
  # Enable/disable individual tools
  bv:
    enabled: true
    timeout: 5s
  
  pt:
    enabled: true
    check_interval: 30s
    stale_threshold: 20m
    auto_kill: true
  
  ubs:
    enabled: true
    min_severity: warning
    fail_on_any_bug: true
  
  cass:
    enabled: true
    search_limit: 5
```

---

## 5. Integration Points with Core Components

### 5.1 Overseer Integration

```go
// internal/core/overseer.go
type Overseer struct {
    bvClient  *bv.Client
    ptClient  *pt.Client
    registry  *tools.Registry
}

func (o *Overseer) poll() {
    // 1. Get ready beads via BV (with fallback)
    readyBeads, err := o.getReadyBeads()
    if err != nil {
        log.Printf("Warning: BV query failed, using fallback: %v", err)
        readyBeads = o.fallbackGetReadyBeads()
    }
    
    // 2. Check for stale workers via PT
    if o.registry.IsAvailable("pt") {
        o.cleanupStaleWorkers()
    }
    
    // 3. Spawn workers for ready beads
    for _, bead := range readyBeads {
        if o.canSpawn(bead) {
            o.spawnWorker(bead)
        }
    }
}

func (o *Overseer) getReadyBeads() ([]beads.Bead, error) {
    if !o.registry.IsAvailable("bv") {
        return nil, errors.New("BV not available")
    }
    
    bvBeads, err := o.bvClient.GetReadyBeads()
    if err != nil {
        return nil, err
    }
    
    // Convert BV format to internal bead format
    return o.convertBVBeads(bvBeads), nil
}

func (o *Overseer) cleanupStaleWorkers() {
    stale, err := o.ptClient.FindStaleProcesses()
    if err != nil {
        log.Printf("PT query failed: %v", err)
        return
    }
    
    for _, proc := range stale {
        log.Printf("Killing stale worker: PID=%d, Runtime=%s, Bead=%s",
            proc.PID, proc.Runtime, proc.BeadID)
        
        if err := o.ptClient.KillProcess(proc.PID); err != nil {
            log.Printf("Failed to kill process %d: %v", proc.PID, err)
            continue
        }
        
        // Mark bead as failed
        o.markWorkerFailed(proc.BeadID, "timeout")
    }
}
```

### 5.2 Worker Integration

```go
// internal/worker/executor.go
type WorkerExecutor struct {
    ubsClient  *ubs.Client
    cassClient *cass.Client
    registry   *tools.Registry
}

func (e *WorkerExecutor) prepareWorker(bead beads.Bead) error {
    // 1. Search for similar sessions via CASS
    if e.registry.IsAvailable("cass") {
        sessions, err := e.cassClient.Search(bead.Title, 3)
        if err == nil && len(sessions.Sessions) > 0 {
            // Add context to worker environment
            context := e.formatCASSContext(sessions)
            os.Setenv("HYDRA_CASS_CONTEXT", context)
        }
    }
    
    return nil
}

func (e *WorkerExecutor) completeWorker(bead beads.Bead, changedFiles []string) error {
    // 1. Run UBS scan
    if e.registry.IsAvailable("ubs") {
        result, err := e.ubsClient.ScanFiles(changedFiles)
        if err != nil {
            log.Printf("UBS scan failed: %v", err)
            // Configurable: fail or continue
            if e.config.UBS.FailOnError {
                return fmt.Errorf("UBS scan failed: %w", err)
            }
        } else if result.BugsFound > 0 {
            // Strict mode: any bugs = failure
            if e.config.UBS.FailOnAnyBug {
                return fmt.Errorf("UBS found %d bugs", result.BugsFound)
            }
        }
    }
    
    // 2. Mark bead complete
    return e.markComplete(bead.ID)
}
```

---

## 6. Error Handling & Resilience

### 6.1 Tool Unavailable

```go
func (c *Client) checkAvailable() bool {
    _, err := exec.LookPath(c.toolName)
    return err == nil
}

// Usage
if !bvClient.checkAvailable() {
    log.Printf("Warning: BV not available, using fallback")
    return fallbackGetReadyBeads()
}
```

### 6.2 Timeout Handling

```go
func (c *Client) execWithTimeout(args []string, timeout time.Duration) ([]byte, error) {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    cmd := exec.CommandContext(ctx, c.toolName, args...)
    
    output, err := cmd.CombinedOutput()
    if ctx.Err() == context.DeadlineExceeded {
        return nil, fmt.Errorf("%s command timed out after %v", c.toolName, timeout)
    }
    
    return output, err
}
```

### 6.3 Partial Failure

```go
func (c *Client) ScanFiles(files []string) (*ScanResult, error) {
    var allBugs []BugReport
    var scannedCount int
    
    for _, file := range files {
        result, err := c.scanSingleFile(file)
        if err != nil {
            log.Printf("Warning: Failed to scan %s: %v", file, err)
            continue // Skip this file, continue with others
        }
        
        allBugs = append(allBugs, result.Bugs...)
        scannedCount++
    }
    
    if scannedCount == 0 {
        return nil, errors.New("no files could be scanned")
    }
    
    return &ScanResult{
        FilesScanned: scannedCount,
        BugsFound:    len(allBugs),
        Bugs:         allBugs,
    }, nil
}
```

---

## 7. Metrics & Observability

### 7.1 Tool Metrics

```go
var (
    toolQueriesTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "hydra_tool_queries_total",
            Help: "Total number of tool queries",
        },
        []string{"tool", "status"},
    )
    
    toolQueryDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "hydra_tool_query_duration_seconds",
            Help: "Tool query duration",
        },
        []string{"tool"},
    )
    
    staleWorkersKilled = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "hydra_stale_workers_killed_total",
            Help: "Total stale workers killed by PT",
        },
    )
    
    bugsFound = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "hydra_bugs_found_total",
            Help: "Total bugs found by UBS",
        },
        []string{"severity", "category"},
    )
)
```

### 7.2 Logging

```go
// Structured logging for tool operations
log.Info().
    Str("tool", "bv").
    Str("project", projectPath).
    Int("ready_beads", len(beads)).
    Dur("duration", duration).
    Msg("Retrieved ready beads from BV")

log.Warn().
    Str("tool", "ubs").
    Str("bead_id", beadID).
    Int("bugs_found", result.BugsFound).
    Msg("UBS found bugs, marking bead failed")
```

---

## 8. Testing Strategy

### 8.1 Mock Clients

```go
// internal/tools/bv/mock_client.go
type MockClient struct {
    ReadyBeads []ReadyBead
    Err        error
}

func (m *MockClient) GetReadyBeads() ([]ReadyBead, error) {
    return m.ReadyBeads, m.Err
}
```

### 8.2 Integration Tests

```go
func TestBVIntegration(t *testing.T) {
    if !bvInstalled() {
        t.Skip("BV not installed")
    }
    
    client := bv.NewClient(testProjectPath)
    beads, err := client.GetReadyBeads()
    
    require.NoError(t, err)
    assert.NotNil(t, beads)
}
```

### 8.3 Fallback Tests

```go
func TestOverseerFallback(t *testing.T) {
    overseer := NewOverseer()
    overseer.registry.tools["bv"] = ToolStatus{Available: false}
    
    beads, err := overseer.getReadyBeads()
    
    // Should fallback to br ready, not fail
    require.NoError(t, err)
    assert.NotEmpty(t, beads)
}
```

---

## 9. Security Considerations

### 9.1 Command Injection Prevention

```go
// NEVER concatenate user input into commands
// BAD:
cmd := exec.Command("ubs", userProvidedFilename) // Risky!

// GOOD:
cmd := exec.Command("ubs", "--json", "--dir", projectPath)
// Sanitize inputs
```

### 9.2 Path Traversal

```go
func sanitizePath(basePath, userPath string) (string, error) {
    fullPath := filepath.Join(basePath, userPath)
    
    // Ensure path stays within basePath
    absPath, err := filepath.Abs(fullPath)
    if err != nil {
        return "", err
    }
    
    absBase, err := filepath.Abs(basePath)
    if err != nil {
        return "", err
    }
    
    if !strings.HasPrefix(absPath, absBase) {
        return "", errors.New("path traversal detected")
    }
    
    return absPath, nil
}
```

### 9.3 Secret Handling

- Tools don't receive API keys (all local)
- No credential passing via CLI arguments
- Environment isolation between workers

---

## 10. Implementation Phases

### Phase 1: Foundation (Week 1)
- [ ] Create `internal/tools/` directory structure
- [ ] Implement tool registry with availability checks
- [ ] Add configuration schema to `hydra.yaml`
- [ ] Implement BV client with fallback

### Phase 2: Worker Safety (Week 1)
- [ ] Implement PT client
- [ ] Add health monitor to overseer
- [ ] Test stale worker detection and cleanup

### Phase 3: Quality Gates (Week 2)
- [ ] Implement UBS client
- [ ] Integrate UBS scan into worker completion
- [ ] Add bug reporting to bead status

### Phase 4: Context Enrichment (Week 2)
- [ ] Implement CASS client
- [ ] Add CASS search to worker startup
- [ ] Pass context to workers via environment

### Phase 5: Observability (Week 3)
- [ ] Add Prometheus metrics
- [ ] Structured logging for all tool operations
- [ ] Dashboard for tool health and usage

### Phase 6: Testing & Documentation (Week 3)
- [ ] Unit tests for all clients
- [ ] Integration tests (with and without tools)
- [ ] Update user documentation

---

## 11. Success Criteria

- ✅ All 4 tools integrate seamlessly via CLI
- ✅ Graceful fallback when tools unavailable
- ✅ BV provides intelligent bead prioritization
- ✅ PT auto-kills workers exceeding 20min
- ✅ UBS blocks completion if bugs found
- ✅ CASS enriches worker context with past sessions
- ✅ Metrics and logging for all tool operations
- ✅ <100ms overhead per tool query
- ✅ Zero security vulnerabilities (injection, traversal)

---

## 12. Future Considerations

### Potential Additions
- **Meta Skill**: Knowledge base queries
- **APR**: PRD refinement before bead creation
- **Desloppify**: Code quality improvement (if needed for non-atomic beads)

### Performance Optimizations
- Cache BV results (5s TTL)
- Parallel tool queries where possible
- Connection pooling if tools add HTTP APIs

### Tool Evolution
- Monitor tool updates for breaking changes
- Version pinning for reproducibility
- Automated integration testing on tool updates

---

## Appendix A: Tool Installation Commands

```bash
# Install all tools at once (via Agent Flywheel)
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/agentic_coding_flywheel_setup/main/install.sh | bash

# Or install individually:

# BV
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/beads_viewer/main/install.sh | bash

# Process Triage
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/process_triage/main/install.sh | bash

# UBS
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/ultimate_bug_scanner/main/install.sh | bash

# CASS
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/cass/main/install.sh | bash
```

## Appendix B: Quick Reference

| Tool | Check Available | Key Command | Typical Use |
|------|-----------------|-------------|-------------|
| BV | `bv --version` | `bv --robot-triage` | Overseer polling |
| PT | `pt --version` | `pt --robot --stale 20m` | Health checks |
| UBS | `ubs --version` | `ubs --json <files>` | Worker completion |
| CASS | `cass --version` | `cass search "query"` | Worker startup |

---

**End of PRD**