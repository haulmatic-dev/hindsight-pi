# Product Requirements Document: External Tool Integrations for Hydra

**Version:** 1.0  
**Date:** 2026-03-09  
**Status:** Draft  

---

## 1. Executive Summary

### 1.1 Overview

This PRD defines the integration of 4 external command-line tools into Hydra's orchestration layer to transform it from a basic task runner into an intelligent, self-improving orchestration platform.

### 1.2 Integrated Tools

| Tool | Repository | Purpose | Primary Role |
|------|------------|---------|--------------|
| **BV (beads_viewer)** | Dicklesworthstone/beads_viewer | Graph analytics, PageRank prioritization, bottleneck detection | Scheduling Intelligence |
| **UBS (ultimate_bug_scanner)** | Dicklesworthstone/ultimate_bug_scanner | Multi-language static analysis with 1000+ bug patterns | Quality Gate |
| **CASS** | Dicklesworthstone/coding_agent_session_search | Session history search across 11+ agent types | Context Enrichment |
| **Enhanced cass_memory** | Dicklesworthstone/cass_memory_system | Pattern validation, trauma guard, continuous learning | Safety & Learning |

### 1.3 Integration Philosophy

- **Shell-based**: All tools invoked via CLI with JSON output (no library dependencies)
- **Graceful degradation**: Continue operating if tools are unavailable
- **Non-blocking**: Tools enhance but don't block the orchestration flow
- **Best-effort operations**: Errors logged but don't disrupt main workflow
- **Configurable**: Behavior adjustable via hydra.yaml

### 1.4 Business Value

| Integration | Cost Saving | Speed Improvement | Quality Improvement |
|-------------|-------------|-------------------|---------------------|
| BV | Better resource allocation | 40% faster critical path completion | Priority-based execution |
| UBS | Fewer bugs in production | Early detection at commit time | 95% quality threshold |
| CASS | Reduced rework | Faster execution with context | Learning from history |
| Enhanced cm | Prevents dangerous operations | Faster validation | Evidence-based decisions |

---

## 2. User Stories

### 2.1 As an Orchestrator Agent, I want intelligent bead prioritization

**Story:** As an orchestrator agent, I want beads to be automatically prioritized based on their impact on the dependency graph, so that critical path items are completed first and downstream work is unblocked faster.

**Acceptance Criteria:**
1. Given multiple ready beads, when the overseer polls for work, then beads_viewer's PageRank scores are used to sort beads by impact
2. Given a bead with high betweenness centrality, when it becomes ready, then it is prioritized as a bottleneck
3. Given a quick-win opportunity (low effort, high impact), when identified by bv, then it is surfaced for immediate execution
4. Given bv is unavailable, when polling for beads, then the system falls back to basic priority-based sorting without error

### 2.2 As a Quality Engineer, I want automated bug detection

**Story:** As a quality engineer, I want all code changes to be automatically scanned for bugs before beads are marked complete, so that technical debt doesn't accumulate and bugs don't reach production.

**Acceptance Criteria:**
1. Given a worker completes a bead, when closing the bead, then UBS scans all changed files
2. Given bugs are found with severity >= warning, when the score is < 95%, then the bead is marked FAILED with bug details
3. Given no bugs are found, when UBS completes scanning, then the bead proceeds to close successfully
4. Given UBS is unavailable, when attempting to scan, then the system logs a warning and allows the bead to close (graceful degradation)
5. Given the bead fails due to bugs, when retrying, then the bug report is included in the worker context for fixing

### 2.3 As a Worker Agent, I want historical context

**Story:** As a worker agent, I want access to similar past sessions and solutions before starting work, so that I can learn from previous attempts and avoid repeating mistakes.

**Acceptance Criteria:**
1. Given a bead is about to be processed, when the worker spawns, then CASS searches for similar sessions based on bead title
2. Given relevant sessions are found, when the worker initializes, then session snippets are included in the worker environment
3. Given similar patterns exist in cass_memory, when context is gathered, then relevant rules and anti-patterns are provided
4. Given no similar sessions exist, when searching CASS, then the worker proceeds without context enrichment
5. Given the session search times out, when exceeding 2 seconds, then the worker proceeds with partial or no context

### 2.4 As a Developer, I want safety guardrails

**Story:** As a developer, I want dangerous operations to be blocked before execution, so that common mistakes like `rm -rf /` or `DROP DATABASE` don't accidentally destroy work.

**Acceptance Criteria:**
1. Given a pattern matches a known trauma (dangerous operation), when cm guard is active, then the operation is blocked with a warning
2. Given a complex architectural change, when starting a bead, then cm validate checks the approach against historical evidence
3. Given a validation fails, when evidence shows the approach often fails, then the system warns before proceeding
4. Given trauma guard blocks an operation, when logging the event, then the block is recorded for audit purposes

### 2.5 As an Operations Engineer, I want zero-downtime configuration

**Story:** As an operations engineer, I want to update Hydra configuration without restarting the service, so that changes take effect immediately without disrupting in-progress work.

**Acceptance Criteria:**
1. Given a configuration file changes, when fsnotify detects the change, then the configuration is reloaded within 5 seconds
2. Given the new configuration is invalid, when validation fails, then the previous configuration remains active and an error is logged
3. Given configuration reload succeeds, when applied, then a log entry shows before/after changes
4. Given new tool settings are added, when hot reload occurs, then new tool clients are initialized without restart

---

## 3. Functional Requirements

### FR-001: BV Client Implementation - Commands

**Priority:** Must Have

Create a Go client for beads_viewer that wraps the CLI commands and parses JSON output.

**Acceptance Criteria:**
1. `bv --robot-triage --format json` - Get full triage with PageRank, quick-wins, blockers
2. `bv --robot-next --format json` - Get single top recommendation
3. `bv --robot-plan --format json` - Get dependency-respecting execution plan

### FR-002: BV Client Implementation - Detection & Timeouts

**Priority:** Must Have

**Acceptance Criteria:**
1. `bv --robot-insights --format json` - Get graph metrics (PageRank, betweenness, cycles)
2. Client auto-detects bv availability via `bv --version`
3. All commands support configurable timeout (default: 2s for queries, 30s for analysis)

### FR-003: BV Client Implementation - Parsing & Degradation

**Priority:** Must Have

**Acceptance Criteria:**
1. JSON responses are parsed into typed structs
2. Graceful degradation returns empty results if bv unavailable
3. Health check endpoint available for monitoring

### FR-004: Overseer Integration with BV - Prioritization

**Priority:** Must Have

Integrate BV prioritization into the overseer's bead selection logic.

**Acceptance Criteria:**
1. Overseer polls `bv --robot-triage` instead of `br ready` when BV is available
2. Beads are sorted by bv's impact score (PageRank × Priority × UnblockBoost)
3. Quick-wins are surfaced when available

### FR-005: Overseer Integration with BV - Fallback

**Priority:** Must Have

**Acceptance Criteria:**
1. Critical path items receive priority boost
2. Fallback to `br ready` if bv is unavailable or times out

### FR-006: Graph Metrics Exposure - API Endpoints

**Priority:** Should Have

Expose graph analytics through the API for monitoring and visualization.

**Acceptance Criteria:**
1. API endpoint `/api/v1/graph/metrics` returns PageRank, betweenness, critical path scores
2. API endpoint `/api/v1/graph/bottlenecks` returns high-betweenness beads
3. Metrics are cached with data_hash to avoid redundant computation

### FR-007: Graph Metrics Exposure - Real-time Updates

**Priority:** Should Have

**Acceptance Criteria:**
1. WebSocket subscription available for real-time updates

### FR-008: UBS Client Implementation - Commands

**Priority:** Must Have

Create a Go client for UBS that scans changed files and parses results.

**Acceptance Criteria:**
1. `ubs --format=json <files>` - Scan specific files
2. `ubs --format=json --staged` - Scan staged files only
3. `ubs --format=json --diff` - Scan changed files vs HEAD

### FR-009: UBS Client Implementation - Detection & Parsing

**Priority:** Must Have

**Acceptance Criteria:**
1. Client auto-detects ubs availability
2. Supports severity filtering (critical, warning, info)
3. Parses JSON output into structured bug reports

### FR-010: UBS Client Implementation - Scoring & Degradation

**Priority:** Must Have

**Acceptance Criteria:**
1. Calculates quality score based on weighted severity
2. Graceful degradation if ubs unavailable

### FR-011: Pre-Completion Quality Gate - Scanning Logic

**Priority:** Must Have

Integrate UBS as a quality gate before marking beads complete.

**Acceptance Criteria:**
1. Before `br close`, UBS scans all files changed during bead execution
2. Score calculation: 100 - (critical_count × 10 + warning_count × 1)
3. If score >= 95%, bead proceeds to close

### FR-012: Pre-Completion Quality Gate - Failure Handling

**Priority:** Must Have

**Acceptance Criteria:**
1. If score < 95%, bead is marked FAILED with bug report
2. Bug report includes: file, line, severity, message, rule ID
3. Configuration allows adjusting threshold (default: 95%)

### FR-013: Worker Integration - Bug Context

**Priority:** Should Have

Include UBS results in worker context for fixing failed beads.

**Acceptance Criteria:**
1. When a bead fails due to UBS, bug details are passed to retry worker
2. Bugs are formatted in prompt context for opencode
3. Severity-based prioritization (critical bugs listed first)

### FR-014: Worker Integration - Suggested Fixes

**Priority:** Should Have

**Acceptance Criteria:**
1. Optional: Include suggested fixes if available from UBS

### FR-015: CASS Client Implementation - Commands

**Priority:** Must Have

Create a Go client for CASS that searches session history.

**Acceptance Criteria:**
1. `cass search "query" --robot --limit N` - Search for sessions
2. `cass view <session-path> --json` - Get full session details
3. `cass health --json` - Check CASS health status

### FR-016: CASS Client Implementation - Detection & Parsing

**Priority:** Must Have

**Acceptance Criteria:**
1. Client auto-detects cass availability via `cass health`
2. Search queries support --limit, --days, --agent filters
3. JSON responses parsed into SessionHit structs

### FR-017: CASS Client Implementation - Health & Degradation

**Priority:** Must Have

**Acceptance Criteria:**
1. Health check with exit code 0 = healthy, 1 = unhealthy
2. Graceful degradation returns empty results if cass unavailable

### FR-018: Pre-Execution Context Enrichment - Search & Environment

**Priority:** Must Have

Query CASS before worker execution to gather historical context.

**Acceptance Criteria:**
1. When overseer spawns worker, CASS searches for sessions matching bead title
2. Top 3 most relevant sessions are included in worker environment
3. Session snippets included as `HYDRA_CASS_CONTEXT` environment variable

### FR-019: Pre-Execution Context Enrichment - Timeouts & Limits

**Priority:** Must Have

**Acceptance Criteria:**
1. Search timeout: 2 seconds (proceed without context if timeout)
2. Content is truncated to limit token usage (configurable max length)

### FR-020: Cross-Agent Learning - Multi-Agent Support

**Priority:** Could Have

Enable learning from sessions across different agent types.

**Acceptance Criteria:**
1. CASS indexes sessions from: opencode, claude_code, cursor, codex, aider, etc.
2. Search results include agent type (source)
3. Weight recent sessions higher (time decay)

### FR-021: Cross-Agent Learning - Filtering

**Priority:** Could Have

**Acceptance Criteria:**
1. Optional: Filter by specific agent types

### FR-022: Validation Gate - Command & Integration

**Priority:** Should Have

Use cm validate to check complex approaches before execution.

**Acceptance Criteria:**
1. `cm validate "approach description" --json` - Validate proposed approach
2. For beads with type "architecture" or "refactor", validate approach before starting
3. Parse verdict: accept/reject/draft with confidence score

### FR-023: Validation Gate - Results & Fallback

**Priority:** Should Have

**Acceptance Criteria:**
1. If rejected and confidence > 0.8, warn with historical evidence
2. Include validation results in worker context
3. Best-effort: don't block if validation fails

### FR-024: Trauma Guard - Commands & Blocking

**Priority:** Should Have

Install trauma guard to block dangerous operations.

**Acceptance Criteria:**
1. `cm guard --install` - Install git hook
2. `cm trauma list` - List active traumas
3. Trauma guard blocks: rm -rf /, DROP DATABASE, git push --force main, etc.

### FR-025: Trauma Guard - Logging & Bypass

**Priority:** Should Have

**Acceptance Criteria:**
1. Blocked operations return error with explanation
2. Audit log records all trauma blocks
3. Optional: Can be temporarily bypassed with reason

### FR-026: Trauma Guard - Scope

**Priority:** Should Have

**Acceptance Criteria:**
1. Works for both worker processes and manual operations

### FR-027: Similar Pattern Discovery - Command & Integration

**Priority:** Could Have

Use cm similar to find related patterns before implementation.

**Acceptance Criteria:**
1. `cm similar "query" --json` - Find semantically similar rules
2. Query cm similar with bead description
3. Include top 3 similar patterns in worker context

### FR-028: Similar Pattern Discovery - Pattern Details

**Priority:** Could Have

**Acceptance Criteria:**
1. Patterns include: content, category, score
2. Helps discover related best practices

### FR-029: Continuous Learning Schedule - Command & Scheduling

**Priority:** Could Have

Automate rule extraction from sessions.

**Acceptance Criteria:**
1. `cm reflect --days 7 --json` - Extract rules from recent sessions
2. Schedule weekly reflection via cron or background goroutine
3. Process sessions from the past 7 days

### FR-030: Continuous Learning Schedule - Automation & Logging

**Priority:** Could Have

**Acceptance Criteria:**
1. Automatically validate and merge new rules
2. Log statistics: sessions processed, rules extracted, rules accepted

### FR-031: Configuration File Watching - Detection & Validation

**Priority:** Should Have

Implement hot reload using fsnotify.

**Acceptance Criteria:**
1. Watch hydra.yaml for changes using fsnotify
2. Detect changes within 5 seconds
3. Validate new configuration before applying

### FR-032: Configuration File Watching - Rollback & Logging

**Priority:** Should Have

**Acceptance Criteria:**
1. If validation fails, keep previous configuration active
2. Log before/after changes when reload succeeds

### FR-033: Dynamic Tool Client Refresh - Reinitialization

**Priority:** Should Have

Refresh tool clients when configuration changes.

**Acceptance Criteria:**
1. When tool configuration changes (bv, ubs, cass, cm), reinitialize clients
2. New timeouts take effect immediately
3. Enabled/disabled states updated without restart

### FR-034: Dynamic Tool Client Refresh - Health & Continuity

**Priority:** Should Have

**Acceptance Criteria:**
1. Health checks re-run after reinitialization
2. No interruption to in-progress workers

---

## 4. Non-Functional Requirements

### NFR-001: Performance Requirements

**Priority:** Must Have

**Acceptance Criteria:**
1. BV triage query latency < 500ms (timeout: 2s)
2. BV plan query latency < 1s (timeout: 5s)
3. UBS scan completes in 5-15s (timeout: 30s)
4. CASS search latency < 500ms (timeout: 2s)
5. cm context lookup < 200ms (timeout: 5s)
6. cm validate completes in 1-2s (timeout: 10s)
7. Config reload < 1s (timeout: 5s)

### NFR-002: Reliability Requirements

**Priority:** Must Have

**Acceptance Criteria:**
1. All tools implement graceful degradation with fallback behaviors
2. Circuit breaker opens after 5 consecutive failures
3. Circuit breaker half-open after 30s timeout
4. Circuit breaker closed after 3 consecutive successes
5. Background health checks run every 30s

### NFR-003: Scalability Requirements

**Priority:** Must Have

**Acceptance Criteria:**
1. Tool clients are thread-safe
2. Support up to 20 concurrent workers
3. Cache BV metrics by data_hash
4. Cache CASS results with 5-minute TTL
5. Cache cm context with 10-minute TTL

### NFR-004: Security Requirements

**Priority:** Must Have

**Acceptance Criteria:**
1. All inputs validated before passing to shell commands
2. Use exec.Command with arguments (not shell strings)
3. Tool API keys stored in environment variables only
4. Audit log all tool invocations with duration
5. Log all configuration changes
6. Log all trauma guard blocks

---

## 5. Technical Considerations

### 5.1 Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Hydra Orchestrator                        │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Integration Layer (internal/tools)          │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │   │
│  │  │ BVClient │ │ UBSClient│ │ CASSClient│ │CMClient │   │   │
│  │  │(shell)   │ │(shell)   │ │(shell)   │ │(shell)   │   │   │
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘   │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                   │
│  ┌───────────────────────────▼─────────────────────────────┐   │
│  │                  Core Orchestration                      │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────────────────┐  │   │
│  │  │ Overseer │  │   DAG    │  │  Hot Reload Manager  │  │   │
│  │  └──────────┘  └──────────┘  └──────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### 5.2 Directory Structure

```
internal/
├── tools/                          # NEW: External tool clients
│   ├── bv/
│   │   ├── client.go              # BV CLI wrapper
│   │   ├── client_test.go         # Unit tests
│   │   └── types.go               # Response types
│   ├── ubs/
│   │   ├── client.go              # UBS CLI wrapper
│   │   ├── client_test.go         # Unit tests
│   │   └── types.go               # Response types
│   ├── cass/
│   │   ├── client.go              # CASS CLI wrapper
│   │   ├── client_test.go         # Unit tests
│   │   └── types.go               # Response types
│   └── registry.go                # Tool registry and health
├── intelligence/                   # EXISTING
│   ├── cass_memory.go             # Enhanced with new methods
│   └── ...
├── core/
│   ├── config.go                  # Add tool configs
│   ├── overseer.go                # Integrate tools
│   └── hot_reload.go              # NEW: fsnotify watcher
└── ...
```

### 5.3 Configuration Schema

```yaml
# hydra.yaml
overseer:
  poll_interval: 5s
  max_workers: 10
  worker_timeout: 5m

intelligence:
  cass_memory:
    enabled: true
    timeout: 10s
    # NEW: validation and guard settings
    validation_enabled: true
    trauma_guard_enabled: true
    
  # NEW: BV configuration
  beads_viewer:
    enabled: true
    timeout: 2s
    plan_timeout: 30s
    
  # NEW: UBS configuration
  ubs:
    enabled: true
    command: "ubs"
    timeout: 30s
    min_severity: warning
    quality_threshold: 95
    fail_on_any_bug: true
    
  # NEW: CASS configuration
  cass:
    enabled: true
    timeout: 2s
    search_limit: 3
    content_max_length: 2000

# Hot reload configuration
hot_reload:
  enabled: true
  debounce_duration: 1s
  watch_paths:
    - "./hydra.yaml"
    - "./hydra-local.yaml"
```

### 5.4 Data Models

#### BV Response Types
```go
type TriageResult struct {
    GeneratedAt   time.Time       `json:"generated_at"`
    DataHash      string          `json:"data_hash"`
    QuickRef      QuickRef        `json:"quick_ref"`
    Recommendations []Recommendation `json:"recommendations"`
    QuickWins       []Recommendation `json:"quick_wins"`
    BlockersToClear []Recommendation `json:"blockers_to_clear"`
}

type Recommendation struct {
    BeadID   string   `json:"bead_id"`
    Score    float64  `json:"score"`
    Reasons  []string `json:"reasons"`
    Command  string   `json:"command"`
}
```

#### UBS Response Types
```go
type UBSScanResult struct {
    Project     string      `json:"project"`
    FilesScanned int        `json:"files_scanned"`
    BugsFound   int         `json:"bugs_found"`
    Bugs        []Bug       `json:"bugs"`
    Score       float64     `json:"score"` // Calculated: 100 - penalties
}

type Bug struct {
    Rule     string  `json:"rule"`
    Severity string  `json:"severity"` // critical, warning, info
    File     string  `json:"file"`
    Line     int     `json:"line"`
    Column   int     `json:"column"`
    Message  string  `json:"message"`
}
```

#### CASS Response Types
```go
type CASSSearchResult struct {
    Count       int          `json:"count"`
    TotalMatches int         `json:"total_matches"`
    Hits        []SessionHit `json:"hits"`
}

type SessionHit struct {
    Agent       string  `json:"agent"`
    Title       string  `json:"title"`
    Content     string  `json:"content"`
    Score       float64 `json:"score"`
    SourcePath  string  `json:"source_path"`
    LineNumber  int     `json:"line_number"`
    CreatedAt   int64   `json:"created_at"`
}
```

---

## 6. Risks and Mitigation

### 6.1 Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Tool unavailability causes workflow failures | Medium | High | Implement graceful degradation for all tools |
| BV timeouts slow down bead polling | Medium | Medium | Use 2s timeout with fallback to br ready |
| UBS false positives block valid work | Low | High | Configurable threshold (95%), allow override |
| CASS returns irrelevant context | Medium | Low | Limit to top 3 results, content truncation |
| Hot reload applies invalid config | Low | High | Validate before applying, rollback on failure |
| Concurrent tool calls cause rate limiting | Low | Medium | Implement circuit breaker, caching |

### 6.2 Operational Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Performance degradation from tool overhead | Medium | Medium | Set aggressive timeouts, monitor latency |
| Configuration complexity increases | High | Medium | Document all options, provide examples |
| Debugging becomes harder with more integrations | Medium | Medium | Comprehensive logging, tracing support |
| Tool version incompatibilities | Medium | Medium | Version checking, compatibility matrix |

### 6.3 Mitigation Strategies

**Graceful Degradation:**
- All tools implement IsAvailable() check
- Return empty results rather than errors
- Log warnings at debug level
- Fallback behaviors documented

**Monitoring:**
- Track tool latency (p50, p95, p99)
- Alert on tool unavailability
- Dashboard for integration health
- Circuit breaker state exposed

**Testing:**
- Unit tests with mock executors
- Integration tests with real tools
- Chaos testing (tool failures)
- Performance benchmarks

---

## 7. MoSCoW Prioritization

### 7.1 Must Have

**beads_viewer:**
- BV client with triage and next commands
- Overseer integration for bead prioritization
- Fallback to br ready

**UBS:**
- UBS client for scanning files
- Pre-completion quality gate (95% threshold)
- Mark beads FAILED if bugs found

**CASS:**
- CASS client for session search
- Pre-execution context enrichment
- Include sessions in worker environment

**Configuration:**
- Tool configuration in hydra.yaml
- Graceful degradation for all tools
- Circuit breaker pattern

### 7.2 Should Have

**beads_viewer:**
- Graph metrics API exposure
- Plan command for batch execution

**UBS:**
- Worker integration with bug details
- Configurable severity thresholds

**Enhanced cass_memory:**
- Validation gate for complex beads
- Trauma guard installation

**Configuration:**
- Hot reload with fsnotify
- Dynamic tool client refresh

### 7.3 Could Have

**beads_viewer:**
- WebSocket subscriptions for real-time updates
- Label-specific triage

**CASS:**
- Cross-agent type filtering
- Session rating/feedback

**Enhanced cass_memory:**
- Similar pattern discovery
- Weekly reflection scheduling

**General:**
- Tool performance dashboard
- Advanced caching strategies

### 7.4 Won't Have (This Phase)

- GUI for tool configuration
- Automatic tool installation
- Multi-region tool deployment
- Custom tool plugins

---

## 8. Success Metrics

### 8.1 BV (Beads Viewer) Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Critical path completion time | 40% faster | Compare before/after integration |
| Blocker clearance rate | > 90% | Track beads that unblock downstream work |
| Quick-win utilization | > 50% | Track quick-wins processed vs available |
| Query latency (p95) | < 500ms | Prometheus histogram |
| Fallback usage | < 5% | Track fallback to br ready |

### 8.2 UBS Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Bugs caught pre-completion | > 100/week | Count bugs found by UBS |
| False positive rate | < 5% | Manual review of blocked beads |
| Average scan time | < 15s | Track UBS execution duration |
| Quality score average | > 98% | Track average score across beads |
| Fallback usage | < 2% | Track when UBS unavailable |

### 8.3 CASS Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Context enrichment rate | > 80% | % of workers receiving CASS context |
| Search latency (p95) | < 500ms | Prometheus histogram |
| Relevant session rate | > 70% | Worker feedback on context usefulness |
| Timeout rate | < 5% | Track searches exceeding 2s |

### 8.4 Enhanced cass_memory Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Validation coverage | > 50% | % of architecture beads validated |
| Trauma blocks | Track | Count of dangerous operations blocked |
| Pattern recording rate | 100% | % of completed beads recording patterns |
| Weekly rules extracted | > 5 | Track cm reflect output |

### 8.5 Overall System Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Bead success rate | > 80% | Current: 73%, Target: 80%+ |
| Configuration reload success | > 99% | Track hot reload success rate |
| System availability | > 99.9% | Overall Hydra uptime |
| Mean time to recovery | < 5min | Time to recover from tool failures |

---

## 9. Research Insights

### 9.1 Codebase Findings

**Current Architecture Strengths:**
- Clean separation of concerns (cmd/, internal/, pkg/)
- Interface-based design for testability
- Established patterns for CLI tool integration
- Graceful degradation already implemented for cass_memory
- Configuration system supports flexible YAML parsing

**Extension Points Identified:**
1. `internal/core/overseer.go:runPreExecutionIntelligence()` - Add BV/CASS enrichment
2. `internal/core/overseer.go:spawnWorkerInternal()` - Pass context to workers
3. `cmd/worker/main.go:closeWorkerBead()` - Add UBS quality gate
4. `internal/core/config.go` - Add tool configuration structs

### 9.2 External Tool Capabilities

**beads_viewer:**
- 9 graph metrics computed (PageRank, betweenness, HITS, etc.)
- Two-phase analysis (instant + async)
- Robot-mode for automation (AVOID bare `bv` - launches TUI)
- Results cached by data_hash

**UBS:**
- 1000+ bug patterns across 9 languages
- Multiple output formats (JSON, SARIF, HTML)
- Staged/diff modes for efficiency
- CI mode with --fail-on-warning

**CASS:**
- Searches across 11+ agent types (opencode, claude, cursor, etc.)
- BM25 + semantic search (MiniLM embeddings)
- <60ms typical query time
- Local-only (no external APIs)

**cass_memory:**
- Context retrieval: `cm context` (main command)
- Validation: `cm validate` (evidence-based)
- Safety: `cm guard` (trauma patterns)
- Learning: `cm reflect` (rule extraction)

### 9.3 Historical Integration Patterns

**Established Patterns:**
1. Shell-based CLI integration with JSON parsing
2. Auto-detection via `--version` or health endpoint
3. Functional options pattern for configuration
4. Mock executor interface for testing
5. Graceful degradation (empty results, not errors)

**Files Modified in Past Integrations:**
- Always: `internal/core/config.go`, `internal/core/overseer.go`
- Always: Create `internal/{package}/client.go`, `client_test.go`
- Often: Add documentation in `docs/PRD/`

### 9.4 Domain Best Practices

**Orchestration:**
- Prefer async integration to avoid latency
- Use graduated quality gates (block/warn/advisory)
- Implement circuit breakers for external calls
- Cache aggressively for repeated queries

**Context Enrichment:**
- Hybrid eager/lazy loading (critical=eager, historical=lazy)
- Tiered caching (fast local, slower remote)
- Limit context size to prevent token overload
- Pre-fetch when bead becomes ready

**Error Handling:**
- Default to permissive (log warning, continue)
- Clear severity levels with documented thresholds
- Feedback loops into metadata
- Historical tracking of quality trends

---

## 10. Implementation Phases

### Phase 1: Foundation (Week 1)

**Goals:** Create directory structure, config, basic clients

**Tasks:**
1. Create `internal/tools/` directory structure
2. Add tool configurations to `internal/core/config.go`
3. Implement ToolRegistry in `internal/tools/registry.go`
4. Create mock executor for testing

**Deliverables:**
- Directory structure in place
- Configuration schema defined
- Tool availability detection working

### Phase 2: BV Integration (Week 1-2)

**Goals:** Intelligent bead prioritization

**Tasks:**
1. Implement BV client (`internal/tools/bv/client.go`)
2. Add BV to overseer polling logic
3. Implement fallback to br ready
4. Write unit tests

**Deliverables:**
- Beads prioritized by PageRank
- Quick-wins surfaced
- Blockers identified
- Tests passing

### Phase 3: UBS Integration (Week 2)

**Goals:** Quality gate implementation

**Tasks:**
1. Implement UBS client (`internal/tools/ubs/client.go`)
2. Add pre-completion scanning to worker
3. Implement 95% quality threshold
4. Include bug reports in failed beads

**Deliverables:**
- Files scanned before bead close
- Bugs caught and reported
- Failed beads include fix instructions
- Tests passing

### Phase 4: CASS Integration (Week 2-3)

**Goals:** Context enrichment from sessions

**Tasks:**
1. Implement CASS client (`internal/tools/cass/client.go`)
2. Add session search before worker spawn
3. Include context in worker environment
4. Write unit tests

**Deliverables:**
- Workers receive historical context
- Similar sessions included
- Timeout handling working
- Tests passing

### Phase 5: Enhanced cass_memory (Week 3)

**Goals:** Safety and validation

**Tasks:**
1. Add validation gate to `internal/intelligence/cass_memory.go`
2. Implement trauma guard installation
3. Add similar pattern discovery
4. Schedule weekly reflection

**Deliverables:**
- Complex beads validated before execution
- Dangerous operations blocked
- Patterns discovered and included
- Tests passing

### Phase 6: Hot Reload (Week 3-4)

**Goals:** Zero-downtime configuration

**Tasks:**
1. Implement fsnotify watcher (`internal/core/hot_reload.go`)
2. Add configuration validation
3. Implement rollback on failure
4. Add dynamic tool client refresh

**Deliverables:**
- Configuration changes applied without restart
- Invalid configs rejected gracefully
- Tool clients refreshed dynamically
- Tests passing

### Phase 7: Testing & Documentation (Week 4)

**Goals:** Comprehensive testing and documentation

**Tasks:**
1. Write integration tests
2. Add chaos testing (tool failures)
3. Performance benchmarks
4. Update AGENTS.md and README
5. Create monitoring dashboard

**Deliverables:**
- All tests passing (>80% coverage)
- Documentation updated
- Monitoring in place
- Production ready

---

## 11. Appendix

### 11.1 Tool Installation

**beads_viewer:**
```bash
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/beads_viewer/main/install.sh | bash
```

**UBS:**
```bash
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/ultimate_bug_scanner/main/install.sh | bash
# Or: brew install dicklesworthstone/tap/ubs
```

**CASS:**
```bash
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/coding_agent_session_search/main/install.sh | bash
```

**cass_memory (cm):**
```bash
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/cass_memory_system/main/install.sh | bash
```

### 11.2 API Endpoints

**New Endpoints:**
- `GET /api/v1/tools/health` - Tool availability status
- `GET /api/v1/graph/metrics` - Graph analytics
- `GET /api/v1/graph/bottlenecks` - Bottleneck beads
- `GET /api/v1/quality/score` - Quality score distribution
- `POST /api/v1/config/reload` - Trigger hot reload

### 11.3 Environment Variables

**New Variables for Workers:**
- `HYDRA_BV_PRIORITY` - PageRank score
- `HYDRA_CASS_CONTEXT` - Session context (JSON)
- `HYDRA_CM_VALIDATION` - Validation results
- `HYDRA_UBS_SCORE` - Quality score from previous attempt

### 11.4 Testing Commands

```bash
# Run all tests
go test ./...

# Run specific package
go test ./internal/tools/bv/... -v
go test ./internal/tools/ubs/... -v
go test ./internal/tools/cass/... -v

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Integration tests
go test -tags=integration ./...
```

---

**End of PRD**
