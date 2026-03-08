# Product Requirements Document: Phase 4 - Worker Logic

**Version:** 1.0  
**Date:** 2026-03-08  
**Status:** Ready for Implementation  

---

## 1. Executive Summary

Phase 4 completes the worker execution logic with intelligent agent selection, robust error handling, and comprehensive retry mechanisms. This phase ensures workers can determine the correct agent type, execute tasks with opencode, handle failures gracefully, and learn from execution history.

### Business Value

- **Higher Success Rates:** Intelligent agent selection matches task to best agent
- **Resilient Execution:** Retry logic handles transient failures
- **Continuous Learning:** Records patterns for future improvements
- **Better Debugging:** Comprehensive error context and logging

---

## 2. Current State

### ✅ Already Implemented (Phases 1-3)
- Basic worker spawn and monitoring
- MCP integration (registration, file reservations, messages)
- cass_memory client (context queries, pattern recording)
- TLDR client (impact analysis)
- Log streaming via MCP

### 🔧 Remaining for Phase 4
1. **Agent Type Determination** - Map bead types to optimal agents
2. **Opencode Execution** - Complete integration with opencode CLI
3. **Error Handling & Retries** - Comprehensive retry logic with backoff
4. **Pattern Recording** - Post-execution learning
5. **Pre-execution Intelligence** - Query cass_memory and TLDR before execution

---

## 3. Functional Requirements

### FR-001: Agent Type Determination

**Description:** Automatically determine the best agent type for a bead.

**Current Implementation:**
```go
agentType = types.AgentType(config.AgentType) // From config
```

**Enhanced Implementation:**
```go
func DetermineAgentType(bead Bead) []AgentType {
    // Priority 1: Explicit bead type mapping
    switch bead.Type {
    case "implement", "requirements":
        return []AgentType{AgentOrchestrator}
    case "test", "test-validate":
        return []AgentType{AgentTestSpecialist}
    case "review":
        return []AgentType{AgentCodeReviewer, AgentAdversarialReviewer}
    case "fix", "bug":
        return []AgentType{AgentFixSpecialist}
    case "deploy":
        return []AgentType{AgentDeploymentSpecialist}
    case "approval-gate":
        return []AgentType{AgentHuman}
    }
    
    // Priority 2: Query cass_memory for historical success
    patterns := cassMemory.QuerySimilar(bead.Title)
    agentSuccess := analyzeAgentSuccess(patterns)
    
    // Priority 3: Content-based detection
    if isTestBead(bead) {
        return []AgentType{AgentTestSpecialist}
    }
    if isFixBead(bead) {
        return []AgentType{AgentFixSpecialist}
    }
    
    // Default
    return []AgentType{AgentOrchestrator}
}
```

**Acceptance Criteria:**
- Map all standard bead types to appropriate agents
- Support multiple agents for review tasks
- Query cass_memory for historical agent performance
- Override via configuration if needed

### FR-002: Pre-Execution Intelligence

**Description:** Query intelligence systems before executing task.

**Requirements:**
```go
func gatherPreExecutionIntelligence(bead Bead) (*IntelligenceContext, error) {
    ctx := &IntelligenceContext{}
    
    // 1. Query cass_memory for patterns
    if cassMemory.IsAvailable() {
        patterns, err := cassMemory.Context(bead.Title)
        if err == nil {
            ctx.Patterns = patterns.RelevantBullets
            ctx.AntiPatterns = patterns.AntiPatterns
        }
    }
    
    // 2. Analyze code impact with TLDR
    files := extractTargetFiles(bead)
    if len(files) > 0 && tldr.IsAvailable() {
        impact, err := tldr.ImpactAnalysis(files[0], 2)
        if err == nil {
            ctx.Impact = impact
            ctx.Scope = impact.Scope
        }
    }
    
    // 3. Semantic search for context
    if tldr.IsAvailable() {
        results, err := tldr.SemanticSearch(bead.Description)
        if err == nil {
            ctx.RelatedFiles = results.Results
        }
    }
    
    return ctx, nil
}
```

**Acceptance Criteria:**
- Query cass_memory for relevant patterns
- Run TLDR impact analysis on target files
- Perform semantic search for additional context
- Compile intelligence bundle for worker
- Graceful handling if services unavailable

### FR-003: Opencode Execution

**Description:** Execute opencode with correct agent and parameters.

**Current Implementation:**
```go
// TODO: Execute with opencode
```

**Complete Implementation:**
```go
func executeWithOpencode(
    bead Bead,
    agentType AgentType,
    intelligence *IntelligenceContext,
    mcpClient *mcp.Client,
) (*ExecutionResult, error) {
    
    // Build opencode command
    cmd := buildOpencodeCommand(bead, agentType, intelligence)
    
    // Execute with timeout and monitoring
    result, err := runWithMonitoring(cmd, mcpClient)
    
    return result, err
}

func buildOpencodeCommand(bead Bead, agentType AgentType, intel *IntelligenceContext) *exec.Cmd {
    args := []string{
        "--agent", string(agentType),
        "--task", bead.ID.String(),
        "--project", bead.Project,
    }
    
    if bead.Repo != "" {
        args = append(args, "--repo", bead.Repo)
    }
    
    // Add intelligence context
    if intel != nil && len(intel.Patterns) > 0 {
        args = append(args, "--context", encodeIntelligence(intel))
    }
    
    // Add retry count for this bead
    args = append(args, "--retry", fmt.Sprintf("%d", getRetryCount(bead.ID)))
    
    return exec.Command("opencode", args...)
}
```

**Opencode Command Format:**
```bash
opencode \
  --agent orchestrator \
  --task hydra-abc123 \
  --project myproject \
  --repo backend \
  --context <encoded-intelligence> \
  --retry 0
```

**Acceptance Criteria:**
- Build correct opencode command with all parameters
- Pass intelligence context to opencode
- Set timeout based on bead complexity
- Capture stdout/stderr
- Return exit code and execution details

### FR-004: Error Handling & Retries

**Description:** Comprehensive retry logic with exponential backoff.

**Retry Policy:**
```go
type RetryPolicy struct {
    MaxRetries      int
    BaseDelay       time.Duration
    MaxDelay        time.Duration
    ExponentialBase float64
}

var DefaultRetryPolicy = RetryPolicy{
    MaxRetries:      3,
    BaseDelay:       5 * time.Second,
    MaxDelay:        5 * time.Minute,
    ExponentialBase: 2.0,
}

func shouldRetry(err error, attempt int, policy RetryPolicy) bool {
    if attempt >= policy.MaxRetries {
        return false
    }
    
    // Retry on transient errors
    if isTransientError(err) {
        return true
    }
    
    // Retry on specific exit codes
    if exitErr, ok := err.(*exec.ExitError); ok {
        code := exitErr.ExitCode()
        // Retry on: 1 (general error), 137 (OOM killed), 143 (SIGTERM)
        return code == 1 || code == 137 || code == 143
    }
    
    return false
}

func calculateBackoff(attempt int, policy RetryPolicy) time.Duration {
    delay := float64(policy.BaseDelay) * math.Pow(policy.ExponentialBase, float64(attempt))
    if delay > float64(policy.MaxDelay) {
        delay = float64(policy.MaxDelay)
    }
    // Add jitter (±25%)
    jitter := (rand.Float64() - 0.5) * 0.5 * delay
    return time.Duration(delay + jitter)
}
```

**Worker Retry Flow:**
```go
func executeWithRetry(
    bead Bead,
    agentType AgentType,
    intelligence *IntelligenceContext,
    mcpClient *mcp.Client,
    policy RetryPolicy,
) (*ExecutionResult, error) {
    
    var lastErr error
    for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
        if attempt > 0 {
            backoff := calculateBackoff(attempt-1, policy)
            log.Printf("⏳ Retry %d/%d for bead %s after %v", 
                attempt, policy.MaxRetries, bead.ID, backoff)
            time.Sleep(backoff)
        }
        
        result, err := executeWithOpencode(bead, agentType, intelligence, mcpClient)
        if err == nil {
            return result, nil
        }
        
        lastErr = err
        
        if !shouldRetry(err, attempt, policy) {
            break
        }
        
        // Record retry in cass_memory
        recordRetryAttempt(bead.ID, attempt, err)
    }
    
    return nil, fmt.Errorf("failed after %d attempts: %w", policy.MaxRetries+1, lastErr)
}
```

**Acceptance Criteria:**
- Retry on transient failures (network, timeout)
- Exponential backoff with jitter
- Configurable max retries and delays
- Record retry attempts in cass_memory
- Don't retry on permanent failures (syntax errors, etc.)

### FR-005: Post-Execution Learning

**Description:** Record successful patterns and outcomes.

**Implementation:**
```go
func recordExecutionResult(
    bead Bead,
    result *ExecutionResult,
    intelligence *IntelligenceContext,
    mcpClient *mcp.Client,
) {
    // 1. Record pattern to cass_memory on success
    if result.Success {
        pattern := buildSuccessPattern(bead, result)
        category := GetCategoryForAgentType(AgentType(bead.Type))
        
        if err := cassMemory.AddPattern(pattern, category); err != nil {
            log.Printf("[WARN] Failed to record pattern: %v", err)
        }
    }
    
    // 2. Record outcome for shown rules
    if intelligence != nil && len(intelligence.PatternIDs) > 0 {
        status := "failure"
        if result.Success {
            status = "success"
        }
        
        if err := cassMemory.RecordOutcome(
            status,
            intelligence.PatternIDs,
            result.Duration,
        ); err != nil {
            log.Printf("[WARN] Failed to record outcome: %v", err)
        }
    }
    
    // 3. Send completion via MCP
    notifyTaskComplete(mcpClient, bead, result.Success, result.Duration)
}

func buildSuccessPattern(bead Bead, result *ExecutionResult) string {
    return fmt.Sprintf(
        "Successfully %s '%s' using %s agent. Duration: %v. "+
        "Key approach: %s",
        bead.Type,
        bead.Title,
        bead.Type,
        result.Duration,
        result.Summary,
    )
}
```

**Acceptance Criteria:**
- Record patterns on successful completion
- Record outcome for cass_memory rules
- Include execution details (duration, approach)
- Send MCP notification
- Best-effort (don't fail if recording fails)

---

## 4. Implementation Plan

### File Changes

```
cmd/worker/main.go
├── Add agent type determination
├── Add pre-execution intelligence gathering
├── Add opencode execution
├── Add retry logic
├── Add post-execution recording
└── Improve error handling

pkg/types/bead.go (minor)
└── Add bead type detection helpers

internal/intelligence/cass_memory.go (minor)
└── Add QuerySimilar method
```

### Key Functions to Add

1. `DetermineAgentType(bead Bead) []AgentType`
2. `gatherPreExecutionIntelligence(bead Bead) (*IntelligenceContext, error)`
3. `executeWithOpencode(bead, agentType, intelligence, mcpClient) (*ExecutionResult, error)`
4. `executeWithRetry(bead, agentType, intelligence, mcpClient, policy) (*ExecutionResult, error)`
5. `recordExecutionResult(bead, result, intelligence, mcpClient)`

---

## 5. Testing Strategy

### Unit Tests
- Agent type determination for all bead types
- Retry logic with various error types
- Backoff calculation with jitter
- Pattern building

### Integration Tests
- End-to-end worker execution
- cass_memory query integration
- TLDR integration
- MCP message flow
- Retry with actual failures

### Error Scenarios
- Network failures
- cass_memory unavailable
- TLDR unavailable
- Opencode failures
- MCP disconnection

---

## 6. Success Metrics

- **Agent Selection Accuracy:** >90% correct agent type
- **Retry Success Rate:** >80% succeed on retry
- **Pattern Recording:** 100% of successful tasks recorded
- **Execution Time:** <5% overhead from intelligence gathering

---

**Duration:** 3-4 days  
**Lines of Code:** ~800 (worker improvements + tests)
