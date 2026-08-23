package main

import (

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/agents"
	piagents "github.com/haulmatic-dev/hydra/internal/agents"
	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/internal/pi"
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// WorkerConfig holds the worker configuration from environment variables
type WorkerConfig struct {
	BeadID          string
	Project         string
	ProjectPath     string
	Repo            string
	AgentType       string
	BeadTitle       string
	BeadDescription string
	Hindsight       *HindsightContext
	HindsightClient HindsightClientConfig
	APIURL          string // daemon registry API (HYDRA_API_URL); empty = disabled
	Pi              PiConfig
	Patterns        PatternsConfig
	MCP             struct {
		Enabled bool
		URL     string
	}
}

// PatternsConfig holds close-time pattern-extraction settings (HYDRA_PATTERNS_*).
type PatternsConfig struct {
	Enabled            bool
	StoragePath        string
	MinConfidence      float64
	MaxPatternsPerBead int
}

// PiConfig holds Pi agent configuration
type PiConfig struct {
	Enabled       bool
	Provider      string
	Model         string
	ThinkingLevel string
	Timeout       time.Duration
	APIKeyName    string
	ConfigDir     string

	// Mode selects the Pi protocol: "rpc" for JSONL rpc mode, anything else
	// (including empty) for the piped text mode. Set from HYDRA_PI_MODE, which
	// the daemon derives from agents.pi.mode.
	Mode string

	// RPCAutoConfirm answers rpc `confirm` UI asks with confirmed:true. Default
	// false denies them — a worker running unreviewed commands because the
	// connector auto-approved is a worse failure than a stalled bead.
	RPCAutoConfirm bool

	// Extensions is the allowlist of Pi extensions to load, already narrowed to
	// this bead's phase by the daemon. Only meaningful in rpc mode: text mode
	// has no extension host, so the flags would be inert.
	Extensions []string
}

// RPCEnabled reports whether this worker should drive Pi over the rpc JSONL
// protocol. Only the exact, case-insensitive value "rpc" enables it, so a typo
// falls back to the text mode that works rather than the one under test. Mirrors
// core.PiConfig.RPCEnabled deliberately: the two ends of the env hand-off must
// agree on what counts as "rpc".
func (c PiConfig) RPCEnabled() bool {
	return strings.ToLower(strings.TrimSpace(c.Mode)) == "rpc"
}

// HindsightClientConfig contains configuration for the Hindsight client
type HindsightClientConfig struct {
	Enabled   bool
	BaseURL   string
	Timeout   time.Duration
	Limit     int
	MaxTokens int
}

// HindsightContext represents context passed from the overseer about related
// execution memories and patterns relevant to the current bead.
type HindsightContext struct {
	// RelatedMemories contains past execution memories that may be relevant
	RelatedMemories []ExecutionMemoryReference `json:"related_memories,omitempty"`

	// ProjectPatterns contains patterns learned from this project
	ProjectPatterns []PatternReference `json:"project_patterns,omitempty"`

	// CrossProjectPatterns contains patterns from other projects that may apply
	CrossProjectPatterns []PatternReference `json:"cross_project_patterns,omitempty"`

	// Metadata contains additional context information
	Metadata map[string]string `json:"metadata,omitempty"`

	// ContextID is a unique identifier for this context snapshot
	ContextID string `json:"context_id,omitempty"`

	// GeneratedAt is when this context was generated
	GeneratedAt time.Time `json:"generated_at,omitempty"`
}

// ExecutionMemoryReference represents a reference to a past execution memory
type ExecutionMemoryReference struct {
	// MemoryID is the unique identifier of the memory
	MemoryID string `json:"memory_id"`

	// BeadID of the referenced execution
	BeadID string `json:"bead_id"`

	// Title of the referenced bead
	Title string `json:"title,omitempty"`

	// Type of the referenced bead
	Type string `json:"type,omitempty"`

	// Success indicates if the referenced execution succeeded
	Success bool `json:"success"`

	// RelevanceScore indicates how relevant this memory is (0.0 - 1.0)
	RelevanceScore float64 `json:"relevance_score,omitempty"`

	// Summary is a brief description of what was learned
	Summary string `json:"summary,omitempty"`
}

// PatternReference represents a pattern that may be relevant to the current task
type PatternReference struct {
	// PatternID is the unique identifier of the pattern
	PatternID string `json:"pattern_id"`

	// Category of the pattern (e.g., "best-practice", "anti-pattern")
	Category string `json:"category"`

	// Description of the pattern
	Description string `json:"description"`

	// SourceProject indicates where this pattern was learned
	SourceProject string `json:"source_project,omitempty"`

	// RelevanceScore indicates how relevant this pattern is (0.0 - 1.0)
	RelevanceScore float64 `json:"relevance_score,omitempty"`
}

// RetryPolicy defines the retry strategy for execution
type RetryPolicy struct {
	MaxRetries      int
	BaseDelay       time.Duration
	MaxDelay        time.Duration
	ExponentialBase float64
}

// DefaultRetryPolicy returns a sensible default retry policy
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:      3,
		BaseDelay:       2 * time.Second,
		MaxDelay:        60 * time.Second,
		ExponentialBase: 2.0,
	}
}

// ExecutionResult represents the outcome of executing a bead
type ExecutionResult struct {
	Success   bool
	ExitCode  int
	Output    string
	Error     string
	Duration  time.Duration
	Attempts  int
	AgentType string
	// Verdict is the parsed HYDRA_VERDICT closure block, when the agent
	// emitted one. Retention prefers it over raw output: it is the
	// distilled outcome, ~200 tokens, and exactly what future runs need.
	Verdict      *Verdict
	Intelligence *IntelligenceContext
}

// Note: IntelligenceContext deprecated - now using skills instead of force-feeding
// Pi agents use cass-memory-insights and tldr-code-intelligence skills on-demand
// This struct kept for backward compatibility in result recording only
type IntelligenceContext struct {
	// Fields removed - agents now query via skills
}

var (
	agentID   string
	agentType types.AgentType
)

// ActivityMonitor tracks activity and implements resettable timeout
type ActivityMonitor struct {
	lastActivity time.Time
	timeout      time.Duration
	mu           sync.RWMutex
	sessionDir   string // optional: pi session dir; file mtime = secondary liveness
}

// NewActivityMonitor creates a new activity monitor
func NewActivityMonitor(timeout time.Duration) *ActivityMonitor {
	return &ActivityMonitor{
		lastActivity: time.Now(),
		timeout:      timeout,
	}
}

// SetSessionDir registers pi's session directory as a secondary liveness
// source. Pi persists every agent event to its session file regardless of
// RPC stdout health; a fresh mtime proves the turn is alive even when the
// stream stalls (observed: kill 75s after the last session-recorded event).
func (am *ActivityMonitor) SetSessionDir(dir string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.sessionDir = dir
}

// RecordActivity records that activity occurred
func (am *ActivityMonitor) RecordActivity() {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.lastActivity = time.Now()
}

// IsTimedOut checks if timeout has elapsed since last activity. When a
// session dir is registered, a session file written after lastActivity
// counts as activity (RPC-stall fallback) and refreshes the clock.
func (am *ActivityMonitor) IsTimedOut() bool {
	am.mu.RLock()
	last := am.lastActivity
	dir := am.sessionDir
	am.mu.RUnlock()
	if time.Since(last) <= am.timeout {
		return false
	}
	if dir != "" {
		if mtime := latestFileMtime(dir); mtime.After(last) {
			am.RecordActivity()
			return false
		}
	}
	return true
}

// latestFileMtime returns the newest mtime among files directly inside dir.
// Best-effort: any error yields the zero time (no liveness claim).
func latestFileMtime(dir string) time.Time {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

// TimeSinceActivity returns time since last activity
func (am *ActivityMonitor) TimeSinceActivity() time.Duration {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return time.Since(am.lastActivity)
}

func main() {
	// Generate unique agent ID based on timestamp and PID
	agentID = generateAgentID()

	// Parse configuration from environment
	config := parseConfig()

	if config.BeadID == "" {
		log.Fatal("HYDRA_BEAD_ID not set")
	}

	// Parse agent type
	agentType = types.AgentType(config.AgentType)

	// Parse bead ID
	beadID, err := types.ParseBeadID(config.BeadID)
	if err != nil {
		log.Fatalf("Invalid bead ID %q: %v", config.BeadID, err)
	}

	// Create bead object
	bead := types.Bead{
		ID:          beadID,
		Project:     config.Project,
		Repo:        config.Repo,
		Type:        config.AgentType,
		Title:       config.BeadTitle,
		Description: config.BeadDescription,
	}

	// Use project path if available, otherwise fall back to project name
	projectPath := config.ProjectPath
	if projectPath == "" {
		projectPath = config.Project
	}

	log.Printf("🚀 Worker starting for bead: %s", beadID)
	log.Printf("   Project: %s (path: %s), Repo: %s, Agent: %s", config.Project, projectPath, config.Repo, config.AgentType)
	log.Printf("   Agent ID: %s", agentID)

	// Registry reporter: heartbeat state to the daemon and drain shepherd
	// commands. Fail-open: no HYDRA_API_URL → no reporting, worker behaves
	// exactly as before.
	reporter := newStateReporter(registry.NewClient(config.APIURL), config.BeadID, config.Project)
	reporter.Start()
	activeReporter = reporter

	// Log Hindsight context if available
	if config.Hindsight != nil {
		logHindsightContext(config.Hindsight)
	} else {
		log.Printf("   Hindsight Context: (not provided)")
	}

	// Validate configuration after parsing
	if validationErrs := validateWorkerConfig(config); len(validationErrs) > 0 {
		log.Printf("❌ Configuration validation failed:")
		for _, err := range validationErrs {
			log.Printf("   - %s", err)
		}
		log.Fatalf("Worker cannot start due to configuration errors")
	}

	// Initialize MCP client
	var mcpClient *mcp.Client
	if config.MCP.Enabled {
		mcpClient = initializeMCP(config, bead)
		if mcpClient != nil {
			defer func() {
				if err := mcpClient.Disconnect(); err != nil {
					log.Printf("[WARN] Failed to disconnect MCP client: %v", err)
				}
			}()
		}
	}

	// Reserve files before execution
	if mcpClient != nil {
		if err := reserveFiles(mcpClient, bead); err != nil {
			log.Printf("[WARN] Failed to reserve files: %v", err)
			// Continue execution even if reservation fails
		}
	}

	// Stream startup log
	streamLog(mcpClient, "info", fmt.Sprintf("Worker started for bead %s", beadID))

	// Record start time
	startTime := time.Now()

	// STEP 1: Determine agent type intelligently
	determinedAgentType := determineAgentType(bead)
	log.Printf("📋 Determined agent type: %s", determinedAgentType)
	streamLog(mcpClient, "info", fmt.Sprintf("Using agent type: %s", determinedAgentType))

	// STEP 2: Execute with retry policy
	retryPolicy := DefaultRetryPolicy()
	log.Printf("⚙️  Executing with retry policy (max %d retries)...", retryPolicy.MaxRetries)
	streamLog(mcpClient, "info", fmt.Sprintf("Starting execution (max %d retries)", retryPolicy.MaxRetries))

	result, err := executeWithRetry(bead, determinedAgentType, mcpClient, retryPolicy, projectPath, config)

	// Calculate duration
	duration := time.Since(startTime)
	if result != nil {
		result.Duration = duration
	}

	// Classify the failure so hydra can distinguish infrastructure
	// problems (rate limits, timeouts) from code-level problems
	// (stubs, build errors).  Infrastructure failures do not spawn
	// fix beads; the scheduler retries them with backoff.
	if result != nil && !result.Success {
		result.ExitCode = classifyFailure(result)
		log.Printf("🔧 Failure classified as exit code %d for bead %s", result.ExitCode, beadID)
	}

	// Determine status
	status := "success"
	exitCode := 0
	if err != nil || result == nil || !result.Success {
		status = "failure"
		exitCode = 1
		if result != nil {
			exitCode = result.ExitCode
		}
	}

	// STEP 4: Record execution results to Hindsight (file-based, post-execution)
	// After Pi agent exits, we write output to temp file and then do retention
	// This is more reliable than real-time streaming and survives crashes
	log.Printf("📝 Recording execution results to Hindsight...")

	// The executeWithPi function already wrote output to temp file
	// Now we do retention with the complete execution data
	if config.HindsightClient.Enabled {
		log.Printf("🧠 Retaining execution memory in Hindsight...")
		retainErr := recordExecutionResultSync(bead, result, config)
		if retainErr != nil {
			log.Printf("[WARN] Failed to retain execution memory: %v", retainErr)
			// Non-fatal: bead completion is more important than retention
		} else {
			log.Printf("✅ Execution memory retained in Hindsight")
		}
	}

	// Close-time pattern extraction from the verdict (compounding knowledge).
	// Independent of Hindsight: stores locally even when the recall service is down.
	extractPatternsAtClose(config, bead, result)

	// STEP 5: Close the bead using beads CLI (best-effort)
	// Auto-close guard: even if Pi exited with error, check if the work is actually done.
	workComplete := false
	if result != nil {
		workComplete = checkWorkComplete(bead, projectPath)
	}

	if exitCode == 0 && result != nil && result.Success {
		if err := closeBeadFromWorker(bead, projectPath, config.Repo, "completed"); err != nil {
			log.Printf("[WARN] Failed to close bead from worker (non-fatal): %v", err)
		} else {
			log.Printf("✅ Successfully closed bead %s with status: completed", beadID)
		}
	} else if workComplete {
		// Auto-close guard: Pi may have timed out or exited non-zero, but files were
		// modified and tests pass. Close the bead to prevent infinite retry loops.
		log.Printf("🛡️  Auto-close guard: Pi exit=%d but work is complete (files modified + tests pass). Closing bead.", exitCode)
		if err := closeBeadFromWorker(bead, projectPath, config.Repo, "completed"); err != nil {
			log.Printf("[WARN] Failed to close bead from worker (non-fatal): %v", err)
		} else {
			log.Printf("✅ Successfully closed bead %s with status: completed (auto-close guard)", beadID)
		}
	} else {
		successStr := "false"
		if result != nil {
			successStr = fmt.Sprintf("%v", result.Success)
		}
		log.Printf("⚠️  Bead %s NOT closed - worker failed (exit: %d, success: %v). Will remain open for retry.",
			beadID, exitCode, successStr)
	}

	streamLog(mcpClient, "info", fmt.Sprintf("Worker completed for bead %s with status %s", beadID, status))

	if result != nil && result.Output != "" {
		log.Printf("📄 Execution output:\n%s", result.Output)
	}

	if result != nil && result.Error != "" {
		log.Printf("❌ Execution error:\n%s", result.Error)
	}

	attempts := 0
	if result != nil {
		attempts = result.Attempts
	}
	log.Printf("✅ Worker completed for bead: %s (status: %s, duration: %v, attempts: %d)",
		beadID, status, duration, attempts)

	// Report terminal state before exiting so the daemon registry reflects
	// the outcome even if the scheduler misses the process exit.
	reporter.Stop(exitCode)

	// Exit with appropriate code
	os.Exit(exitCode)
}

// generateAgentID creates a unique agent ID
func generateAgentID() string {
	return fmt.Sprintf("worker-%d-%d", os.Getpid(), time.Now().UnixNano())
}

// parseConfig parses configuration from environment variables
// Supports both legacy individual env vars and new HYDRA_BEAD_JSON for full bead context
func parseConfig() WorkerConfig {
	var config WorkerConfig

	// Check for full bead JSON first (new enhanced context)
	if beadJSON := os.Getenv("HYDRA_BEAD_JSON"); beadJSON != "" {
		var bead types.Bead
		if err := json.Unmarshal([]byte(beadJSON), &bead); err == nil {
			// Successfully parsed full bead JSON
			config.BeadID = string(bead.ID)
			config.Project = bead.Project
			config.Repo = bead.Repo
			// Use actual bead.Type instead of AgentType env var
			config.AgentType = bead.Type
			config.BeadTitle = bead.Title
			config.BeadDescription = bead.Description
			// Note: Metadata, Labels, Dependencies are available in the bead object
			// but not in WorkerConfig. They're passed via the full bead object.
		} else {
			log.Printf("[WARN] Failed to parse HYDRA_BEAD_JSON: %v", err)
		}
	}

	// Legacy env vars (override JSON values if explicitly set)
	if v := os.Getenv("HYDRA_BEAD_ID"); v != "" {
		config.BeadID = v
	}
	if v := os.Getenv("HYDRA_PROJECT"); v != "" {
		config.Project = v
	}
	if v := os.Getenv("HYDRA_PROJECT_PATH"); v != "" {
		config.ProjectPath = v
	}
	if v := os.Getenv("HYDRA_REPO"); v != "" {
		config.Repo = v
	}
	if v := os.Getenv("HYDRA_API_URL"); v != "" {
		config.APIURL = v
	}
	// Only use legacy AGENT_TYPE if JSON wasn't parsed or bead.Type was empty
	if v := os.Getenv("HYDRA_AGENT_TYPE"); v != "" && config.AgentType == "" {
		config.AgentType = v
	}
	if v := os.Getenv("HYDRA_BEAD_TITLE"); v != "" {
		config.BeadTitle = v
	}
	if v := os.Getenv("HYDRA_BEAD_DESCRIPTION"); v != "" {
		config.BeadDescription = v
	}

	// Parse Hindsight context from environment variable
	config.Hindsight = parseHindsightContext()

	// Parse Hindsight client configuration
	config.HindsightClient = parseHindsightClientConfig()

	// Parse Pi configuration
	config.Pi = parsePiConfig()

	// Default agent type if still empty
	if config.AgentType == "" {
		config.AgentType = "orchestrator"
	}

	// Parse MCP configuration
	config.MCP.URL = os.Getenv("HYDRA_MCP_URL")
	if config.MCP.URL == "" {
		config.MCP.URL = "ws://127.0.0.1:8765"
	}

	enabledStr := os.Getenv("HYDRA_MCP_ENABLED")
	if enabledStr == "" {
		config.MCP.Enabled = true
	} else {
		config.MCP.Enabled = strings.ToLower(enabledStr) != "false" &&
			enabledStr != "0" &&
			enabledStr != "no" &&
			enabledStr != "off"
	}

	// Close-time pattern extraction (HYDRA_PATTERNS_*). Fail-open: absent or
	// "false" disables; extractor errors never affect bead completion.
	config.Patterns.Enabled = strings.EqualFold(os.Getenv("HYDRA_PATTERNS_ENABLED"), "true")
	config.Patterns.StoragePath = os.Getenv("HYDRA_PATTERNS_STORAGE_PATH")
	if v := os.Getenv("HYDRA_PATTERNS_MIN_CONFIDENCE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			config.Patterns.MinConfidence = f
		}
	}
	if v := os.Getenv("HYDRA_PATTERNS_MAX_PATTERNS_PER_BEAD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			config.Patterns.MaxPatternsPerBead = n
		}
	}

	return config
}

// parseHindsightContext parses the HYDRA_HISTORIESIGHT_CONTEXT environment variable
// and deserializes it from JSON format. Returns nil if the variable is not set or
// parsing fails.
func parseHindsightContext() *HindsightContext {
	contextJSON := os.Getenv("HYDRA_HISTORIESIGHT_CONTEXT")
	if contextJSON == "" {
		return nil
	}

	var ctx HindsightContext
	if err := json.Unmarshal([]byte(contextJSON), &ctx); err != nil {
		log.Printf("[WARN] Failed to parse HYDRA_HISTORIESIGHT_CONTEXT: %v", err)
		return nil
	}

	return &ctx
}

// parseHindsightClientConfig parses Hindsight client configuration from environment variables
func parseHindsightClientConfig() HindsightClientConfig {
	var cfg HindsightClientConfig

	// Check if Hindsight is enabled
	enabledStr := os.Getenv("HYDRA_HISTORIESIGHT_ENABLED")
	cfg.Enabled = enabledStr != "" && enabledStr != "false" && enabledStr != "0"

	// Parse base URL
	cfg.BaseURL = os.Getenv("HYDRA_HISTORIESIGHT_BASE_URL")
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:8000" // Default Hindsight port
	}

	// Parse timeout (default 30s for async operations)
	timeoutStr := os.Getenv("HYDRA_HISTORIESIGHT_TIMEOUT")
	if timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil {
			cfg.Timeout = d
		} else {
			cfg.Timeout = 30 * time.Second
		}
	} else {
		cfg.Timeout = 30 * time.Second
	}

	// Parse limit
	limitStr := os.Getenv("HYDRA_HISTORIESIGHT_LIMIT")
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil {
			cfg.Limit = n
		} else {
			cfg.Limit = 5
		}
	} else {
		cfg.Limit = 5
	}

	// Parse min score
	// Token budget for recall. HYDRA_HISTORIESIGHT_MAX_TOKENS supersedes the old
	// MIN_SCORE variable, which mapped to a field the recall API never accepted.
	cfg.MaxTokens = 800
	if v := os.Getenv("HYDRA_HISTORIESIGHT_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxTokens = n
		}
	}

	return cfg
}

// parsePiConfig parses Pi agent configuration from environment variables
func parsePiConfig() PiConfig {
	var cfg PiConfig

	// Pi is always enabled (sole supported agent)
	cfg.Enabled = true

	// Parse provider
	cfg.Provider = os.Getenv("HYDRA_PI_PROVIDER")
	if cfg.Provider == "" {
		cfg.Provider = "kimi-coding" // Default provider
	}

	// Parse model
	cfg.Model = os.Getenv("HYDRA_PI_MODEL")
	if cfg.Model == "" {
		cfg.Model = "kimi-for-coding" // Default model
	}

	// Parse thinking level
	cfg.ThinkingLevel = os.Getenv("HYDRA_PI_THINKING_LEVEL")
	if cfg.ThinkingLevel == "" {
		cfg.ThinkingLevel = "on" // Default thinking level
	}

	// Parse timeout
	timeoutStr := os.Getenv("HYDRA_PI_TIMEOUT")
	if timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil {
			cfg.Timeout = d
		} else {
			cfg.Timeout = 10 * time.Minute
		}
	} else {
		cfg.Timeout = 10 * time.Minute
	}

	// Parse protocol mode. Unset means text mode, which is today's behaviour.
	cfg.Mode = os.Getenv("HYDRA_PI_MODE")

	// Parse the rpc confirm policy. Only an explicit "true"/"1" opts in; any
	// unparseable value keeps the safe deny-by-default.
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HYDRA_PI_RPC_AUTO_CONFIRM"))) {
	case "true", "1", "yes":
		cfg.RPCAutoConfirm = true
	default:
		cfg.RPCAutoConfirm = false
	}

	// Parse the per-phase extension allowlist chosen by the daemon.
	cfg.Extensions = parseExtensionList(os.Getenv("HYDRA_PI_EXTENSIONS"))

	// Parse API key name
	cfg.APIKeyName = os.Getenv("HYDRA_PI_API_KEY_NAME")
	if cfg.APIKeyName == "" {
		cfg.APIKeyName = "KIMI_API_KEY" // Default API key name
	}

	// Parse config directory
	cfg.ConfigDir = os.Getenv("HYDRA_PI_CONFIG_DIR")

	return cfg
}

// logHindsightContext logs the hindsight context information
func logHindsightContext(ctx *HindsightContext) {
	if ctx == nil {
		return
	}

	log.Printf("   Hindsight Context: %s (generated: %s)", ctx.ContextID, ctx.GeneratedAt.Format(time.RFC3339))

	if len(ctx.RelatedMemories) > 0 {
		log.Printf("   Related Memories: %d", len(ctx.RelatedMemories))
		for _, mem := range ctx.RelatedMemories {
			status := "✓"
			if !mem.Success {
				status = "✗"
			}
			log.Printf("     %s %s (relevance: %.2f) - %s", status, mem.BeadID, mem.RelevanceScore, mem.Summary)
		}
	}

	if len(ctx.ProjectPatterns) > 0 {
		log.Printf("   Project Patterns: %d", len(ctx.ProjectPatterns))
	}

	if len(ctx.CrossProjectPatterns) > 0 {
		log.Printf("   Cross-Project Patterns: %d", len(ctx.CrossProjectPatterns))
	}
}

// validateWorkerConfig validates the worker configuration and returns a list of errors
// It handles graceful error reporting for missing or malformed context
func validateWorkerConfig(config WorkerConfig) []string {
	var errors []string

	// Validate required fields
	if config.BeadID == "" {
		errors = append(errors, "HYDRA_BEAD_ID is required (set HYDRA_BEAD_ID or HYDRA_BEAD_JSON)")
	} else {
		// Validate bead ID format
		if _, err := types.ParseBeadID(config.BeadID); err != nil {
			errors = append(errors, fmt.Sprintf("Invalid bead ID format %q: %v", config.BeadID, err))
		}
	}

	// Validate project (warn but don't fail if missing)
	if config.Project == "" {
		log.Printf("[WARN] HYDRA_PROJECT not set, may cause issues with bead closure")
	}

	// Validate project path (warn if missing)
	if config.ProjectPath == "" {
		log.Printf("[WARN] HYDRA_PROJECT_PATH not set, will use project name as fallback")
	}

	// Validate agent type
	if config.AgentType == "" {
		log.Printf("[WARN] Agent type not specified, defaulting to 'orchestrator'")
	}

	return errors
}

// initializeMCP initializes and connects the MCP client
func initializeMCP(config WorkerConfig, bead types.Bead) *mcp.Client {
	client, err := mcp.NewClient(config.MCP.URL, agentID)
	if err != nil {
		log.Printf("[WARN] Failed to create MCP client: %v", err)
		return nil
	}

	if err := client.Connect(); err != nil {
		log.Printf("[WARN] Failed to connect to MCP: %v", err)
		return nil
	}

	// Register with MCP
	metadata := mcp.RegistrationMetadata{
		AgentID:   agentID,
		BeadID:    bead.ID.String(),
		Type:      string(agentType),
		Project:   bead.Project,
		Repo:      bead.Repo,
		StartTime: time.Now(),
	}

	if err := client.Register(metadata); err != nil {
		log.Printf("[WARN] Failed to register with MCP: %v", err)
	} else {
		log.Printf("✅ Registered with MCP as %s", agentID)
	}

	return client
}

// reserveFiles reserves target files before execution
func reserveFiles(client *mcp.Client, bead types.Bead) error {
	// Extract target files from bead
	files := extractTargetFiles(bead)
	if len(files) == 0 {
		log.Println("ℹ️ No target files to reserve")
		return nil
	}

	// Broadcast file reservation message to all agents
	for _, file := range files {
		payload := map[string]interface{}{
			"bead_id":     bead.ID.String(),
			"file_path":   file,
			"reserved_at": time.Now(),
			"expires_at":  time.Now().Add(30 * time.Minute),
		}

		if err := client.Broadcast(mcp.MsgTypeFileReserved, payload); err != nil {
			return fmt.Errorf("failed to broadcast file reservation for %s: %w", file, err)
		}
	}

	log.Printf("🔒 Reserved %d files (expires: %s)", len(files), time.Now().Add(30*time.Minute).Format(time.RFC3339))
	return nil
}

// extractTargetFiles extracts target files from bead metadata or description
func extractTargetFiles(bead types.Bead) []string {
	var files []string

	// Check metadata for target files
	if bead.Metadata != nil {
		if targetFiles, ok := bead.Metadata["target_files"]; ok {
			// Parse JSON array of files
			if err := json.Unmarshal([]byte(targetFiles), &files); err != nil {
				// Try comma-separated
				files = parseCommaSeparated(targetFiles)
			}
		}

		// Check for single target file
		if targetFile, ok := bead.Metadata["target_file"]; ok && targetFile != "" {
			files = append(files, targetFile)
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	unique := make([]string, 0, len(files))
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}

	return unique
}

// parseCommaSeparated parses a comma-separated string into a slice
func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}

	var result []string
	for _, part := range bytes.Split([]byte(s), []byte(",")) {
		trimmed := bytes.TrimSpace(part)
		if len(trimmed) > 0 {
			result = append(result, string(trimmed))
		}
	}
	return result
}

// determineAgentType intelligently determines the best agent type for a bead
func determineAgentType(bead types.Bead) string {
	// First, check if agent type is explicitly set in metadata
	if bead.Metadata != nil {
		if agentType, ok := bead.Metadata["agent_type"]; ok && agentType != "" {
			return agentType
		}
	}

	// Use bead type to determine agent type via AgentTypeMap
	agentTypes := types.DetermineAgentTypes(bead.Type)
	if len(agentTypes) > 0 {
		return string(agentTypes[0])
	}

	// Default to orchestrator
	return "orchestrator"
}

// executeWithRetry executes the bead with exponential backoff retry logic
func executeWithRetry(
	bead types.Bead,
	agentType string,
	mcpClient *mcp.Client,
	policy RetryPolicy,
	projectPath string,
	config WorkerConfig,
) (*ExecutionResult, error) {
	var lastErr error
	var result *ExecutionResult

	// Reflexion: each retry's prompt carries the previous attempt's failure so
	// the agent diagnoses instead of re-running the identical prompt blind
	// (observed: fix-beads re-implemented the same file 9x divergently, each
	// attempt knowing nothing of the last). Seeded from HYDRA_PREV_FAILURE so
	// scheduler-level respawns (new worker process) inherit the prior
	// worker's failure too.
	prevFailure := os.Getenv("HYDRA_PREV_FAILURE")
	var cassBullets []string

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		if attempt == 0 {
			// Stagger concurrent workers' Pi cold-starts. Pi resolves its
			// provider list via a remote fetch on startup; several workers
			// launching Pi at once trip spurious "Unknown provider" errors.
			// A small random delay desyncs them.
			time.Sleep(time.Duration(rand.Intn(2500)) * time.Millisecond)
		}
		if attempt > 0 {
			backoff := calculateBackoff(attempt, policy)
			log.Printf("⏳ Retry %d/%d: waiting %v before next attempt...", attempt, policy.MaxRetries, backoff)
			streamLog(mcpClient, "info", fmt.Sprintf("Retry %d/%d: waiting %v", attempt, policy.MaxRetries, backoff))
			time.Sleep(backoff)
		}

		log.Printf("🔄 Execution attempt %d/%d", attempt+1, policy.MaxRetries+1)
		streamLog(mcpClient, "info", fmt.Sprintf("Execution attempt %d/%d", attempt+1, policy.MaxRetries+1))

		// Query Hindsight + CASS for context enrichment before execution
		// (only on first attempt; CASS bullets are reused across attempts)
		var hindsightContext *agents.ContextEnrichment
		if attempt == 0 {
			hindsightContext = queryHindsightForContext(bead, config, mcpClient)
			cassBullets = queryCASSBullets(bead)
		}

		// Execute with Pi agent (sole supported agent). Wrapped with a fast
		// inner retry for transient Pi-startup flakes (provider-list fetch),
		// which cost no LLM tokens and usually clear within a couple tries.
		extras := promptExtras{PrevFailure: prevFailure, CassBullets: cassBullets}
		result, lastErr = executePiWithStartupRetry(bead, config.Pi, mcpClient, projectPath, hindsightContext, extras)
		result.Attempts = attempt + 1

		if lastErr != nil || (result != nil && !result.Success) {
			prevFailure = summarizeFailure(result, lastErr)
		}

		if lastErr == nil && result.Success {
			log.Printf("✅ Execution succeeded on attempt %d", attempt+1)
			streamLog(mcpClient, "info", fmt.Sprintf("Execution succeeded on attempt %d", attempt+1))
			return result, nil
		}

		if lastErr != nil {
			log.Printf("❌ Execution failed on attempt %d: %v", attempt+1, lastErr)
			streamLog(mcpClient, "error", fmt.Sprintf("Execution failed (attempt %d): %v", attempt+1, lastErr))
		}

		if result != nil && !result.Success {
			log.Printf("❌ Execution returned non-success on attempt %d (exit code: %d)", attempt+1, result.ExitCode)
			streamLog(mcpClient, "error", fmt.Sprintf("Execution failed (attempt %d, exit: %d)", attempt+1, result.ExitCode))
		}

		// Check if we should retry
		if !shouldRetry(lastErr, attempt, policy) {
			log.Printf("🛑 Not retrying: error not retryable or max retries reached")
			streamLog(mcpClient, "warn", "Not retrying: error not retryable or max retries reached")
			break
		}
	}

	// All retries exhausted
	if lastErr != nil {
		return result, fmt.Errorf("execution failed after %d attempts: %w", result.Attempts, lastErr)
	}

	return result, fmt.Errorf("execution failed after %d attempts", result.Attempts)
}

// quotaErrorMarkers identify an exhausted API quota, billing limit, or
// terminated access — infrastructure conditions that no code change and no
// retry can fix. The generic rate-limit markers ("429", "rate_limit") do NOT
// match these: Kimi returns quota exhaustion as HTTP 403 with a
// permission_error / access_terminated_error payload (observed live
// 2026-07-27, where it was misclassified as a code failure and spawned 36
// useless fix beads, each retried 4x — a feedback loop that burned more of
// the very quota that was already exhausted).
// transientOverloadMarkers identify provider CAPACITY errors, which are not the
// same thing as being rate limited for sending too much.
//
// Kimi returns "The engine is currently overloaded, please try again later" as a
// 429 with type rate_limit_error. Classifying that as a rate limit is what froze
// the fleet on 2026-07-28: three of them inside ten minutes tripped the circuit
// breaker for fifteen minutes, and because fallback_provider equals the primary
// there was nothing to fail over to, so the spawn gate paused every worker —
// "skipping spawns for 11m32s" — over an error whose own message says to try
// again shortly. Pi already retries it internally (auto_retry_start).
//
// Measured the same day: 9/9 sequential turns succeeded against the same
// provider with no 429 at all, which is what an intermittent capacity blip looks
// like rather than a throughput ceiling we are exceeding.
var transientOverloadMarkers = []string{
	"engine is currently overloaded",
	"overloaded",
	"try again later",
	"service unavailable",
	"503",
}

// isTransientOverload reports whether the provider was momentarily unavailable
// rather than rate-limiting us. Checked BEFORE the generic rate-limit markers,
// because the payload carries both.
func isTransientOverload(errStr string) bool {
	s := strings.ToLower(errStr)
	for _, m := range transientOverloadMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// concurrencyLimitMarkers identify a provider refusing a request because too
// many of OUR OWN requests are already in flight — a ceiling on parallelism,
// not on volume. The remedy is fewer concurrent workers for a few seconds, and
// the AIMD governor (Shepherd B1) exists precisely to apply it.
//
// This must be checked BEFORE isQuotaError, because Kimi returns the
// concurrency refusal with the SAME type as genuine quota exhaustion:
// HTTP 403 with type access_terminated_error. Observed live 2026-07-30:
//
//	{"error":{"message":"You've reached your concurrent request limit. Please
//	 wait for your ongoing requests to finish and try again.",
//	 "type":"access_terminated_error"}}
//
// Classified as quota, that message tripped a 30-minute fleet-wide spawn pause
// 37 times in one day and starved a dogfood run to a ~10% duty cycle, while the
// account's quota was fine the whole time and a single sequential probe
// succeeded. The provider's own message says to wait for in-flight requests to
// finish — which is a concurrency instruction, not a billing one.
var concurrencyLimitMarkers = []string{
	"concurrent request limit",
	"concurrency limit",
	"too many concurrent",
	"max concurrent",
}

// isConcurrencyLimit reports whether the provider rejected the call for having
// too many simultaneous requests. Callers treat it as retryable capacity
// pressure (exit 7): it feeds the concurrency governor and must never trip the
// provider circuit breaker.
func isConcurrencyLimit(errStr string) bool {
	s := strings.ToLower(errStr)
	for _, m := range concurrencyLimitMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

var quotaErrorMarkers = []string{
	"usage limit",
	"billing cycle",
	"quota",
	"permission_error",
	"access_terminated",
	"insufficient_quota",
	"upgrade your plan",
}

// isQuotaError reports whether the error text indicates an exhausted quota or
// billing limit. Callers treat it as infrastructure: never retried inside the
// worker (every attempt is another rejected call) and never turned into a fix
// bead (the scheduler retries the bead with backoff instead).
func isQuotaError(errStr string) bool {
	s := strings.ToLower(errStr)
	for _, m := range quotaErrorMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// shouldRetry determines if an error should be retried
func shouldRetry(err error, attempt int, policy RetryPolicy) bool {
	// Don't retry if we've exhausted all retries
	if attempt >= policy.MaxRetries {
		return false
	}

	if err == nil {
		return false
	}

	// Quota/billing exhaustion: retrying only produces more rejected calls
	// against an already-exhausted quota. Fail fast and let the scheduler
	// back the bead off instead.
	if isQuotaError(err.Error()) {
		return false
	}

	// Don't retry certain error types
	errStr := err.Error()
	nonRetryableErrors := []string{
		"permission denied",
		"not found",
		"invalid",
		"unauthorized",
		"authentication failed",
		"bad request",
	}

	for _, nonRetryable := range nonRetryableErrors {
		if strings.Contains(strings.ToLower(errStr), nonRetryable) {
			return false
		}
	}

	return true
}

// calculateBackoff calculates the delay before the next retry attempt
func calculateBackoff(attempt int, policy RetryPolicy) time.Duration {
	if attempt <= 0 {
		return 0
	}

	// Calculate exponential backoff: baseDelay * (exponentialBase ^ attempt)
	backoff := float64(policy.BaseDelay) * math.Pow(policy.ExponentialBase, float64(attempt-1))

	// Add jitter (±25%) to prevent thundering herd
	jitter := backoff * 0.25 * (2*rand.Float64() - 1)
	backoff += jitter

	// Cap at max delay
	if backoff > float64(policy.MaxDelay) {
		backoff = float64(policy.MaxDelay)
	}

	return time.Duration(backoff)
}

// promptExtras carries per-attempt prompt enrichments: the previous attempt's
// failure summary (Reflexion) and distilled CASS playbook rules.
type promptExtras struct {
	PrevFailure string
	CassBullets []string
}

// summarizeFailure distills a failed attempt into a short blob the next
// attempt's prompt can learn from: exit code plus the tail of the most
// informative output stream.
func summarizeFailure(result *ExecutionResult, err error) string {
	var b strings.Builder
	if result != nil {
		fmt.Fprintf(&b, "exit code %d.", result.ExitCode)
		detail := strings.TrimSpace(result.Error)
		if detail == "" {
			detail = strings.TrimSpace(result.Output)
		}
		if detail != "" {
			if len(detail) > 400 {
				detail = "..." + detail[len(detail)-400:]
			}
			b.WriteString(" ")
			b.WriteString(detail)
		}
	}
	if err != nil {
		if b.Len() > 0 {
			b.WriteString(" | ")
		}
		msg := err.Error()
		if len(msg) > 200 {
			msg = msg[:200]
		}
		b.WriteString(msg)
	}
	return b.String()
}

// queryCASSBullets asks the CASS playbook (cm CLI) for distilled rules
// relevant to the bead and returns up to 3 high-confidence bullet contents.
// This makes institutional-knowledge injection deterministic instead of
// hoping the agent chooses to run `cm context` itself. Fails open: no cm
// binary, timeout, or unparseable output → no bullets.
func queryCASSBullets(bead types.Bead) []string {
	if _, err := exec.LookPath("cm"); err != nil {
		return nil
	}
	query := bead.Title
	if bead.Description != "" {
		desc := bead.Description
		if len(desc) > 80 {
			desc = desc[:80]
		}
		query += " " + desc
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "cm", "context", query, "--json").Output()
	if err != nil {
		return nil
	}
	bullets := parseCASSBullets(out)
	if len(bullets) > 0 {
		log.Printf("📚 Injecting %d CASS playbook rule(s) into prompt", len(bullets))
	}
	return bullets
}

// parseCASSBullets extracts the best playbook rules from `cm context --json`
// output. Real shape (verified live): {"data":{"relevantBullets":[{"content",
// "finalScore","deprecated",...}]}}. Bullets are pre-ranked; scores start low
// (~0.4) and mature with feedback, so the gate is a noise floor (0.3), not
// the 0.7 "high confidence" bar the docs suggest — that bar filtered out
// every real rule in the live store.
func parseCASSBullets(out []byte) []string {
	var doc struct {
		Data struct {
			RelevantBullets []struct {
				Content    string  `json:"content"`
				FinalScore float64 `json:"finalScore"`
				Deprecated bool    `json:"deprecated"`
			} `json:"relevantBullets"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &doc) != nil {
		return nil
	}
	var res []string
	for _, b := range doc.Data.RelevantBullets {
		if b.Deprecated || b.FinalScore < 0.3 || strings.TrimSpace(b.Content) == "" {
			continue
		}
		res = append(res, strings.TrimSpace(b.Content))
		if len(res) == 3 {
			break
		}
	}
	return res
}

// executePiWithStartupRetry wraps executeWithPi with fast, jittered retries for
// transient Pi *startup* failures. Pi resolves its provider list via a remote
// fetch on startup; when several workers launch Pi concurrently that fetch
// flakes and Pi reports a spurious "Unknown provider" for an otherwise-valid
// provider. These failures happen before any LLM call, so they cost no tokens
// and are cheap to retry quickly. Non-startup failures (real execution errors,
// rate limits mid-run) return immediately to the caller's token-aware retry
// loop.
func executePiWithStartupRetry(
	bead types.Bead,
	piConfig PiConfig,
	mcpClient *mcp.Client,
	projectPath string,
	hindsightContext *agents.ContextEnrichment,
	extras promptExtras,
) (*ExecutionResult, error) {
	const maxStartupRetries = 6
	var result *ExecutionResult
	var err error
	for i := 0; ; i++ {
		result, err = executeWithPi(bead, piConfig, mcpClient, projectPath, hindsightContext, extras)
		if i >= maxStartupRetries || !isTransientStartupFailure(result, err) {
			return result, err
		}
		delay := time.Duration(300+rand.Intn(1500)) * time.Millisecond
		log.Printf("⏳ Transient Pi startup flake (provider fetch) on inner attempt %d/%d — retrying in %v",
			i+1, maxStartupRetries, delay)
		time.Sleep(delay)
	}
}

// isTransientStartupFailure reports whether a Pi failure is a no-token startup
// flake (provider-list fetch) rather than a real error worth surfacing.
func isTransientStartupFailure(result *ExecutionResult, err error) bool {
	var sb strings.Builder
	if err != nil {
		sb.WriteString(err.Error())
	}
	if result != nil {
		sb.WriteString(" ")
		sb.WriteString(result.Error)
		sb.WriteString(" ")
		sb.WriteString(result.Output)
	}
	hay := strings.ToLower(sb.String())
	return strings.Contains(hay, "unknown provider") ||
		strings.Contains(hay, "use --list-models")
}

// defaultActivityTimeout returns how long a worker tolerates total silence from
// Pi before killing it.
//
// The two protocols warrant different patience because they emit at very
// different densities. In text mode Pi streams prose, and a long tool run can
// legitimately go quiet for minutes, so 10 minutes is a fair backstop. In rpc
// mode every step is an event — tool_execution_start/update/end, message_update,
// extension_ui_request — so a working session is never quiet for long, and
// silence means the provider request has hung.
//
// That hang is real and measured: on 2026-07-28 a worker logged "prompt sent over
// rpc" at 07:50:09 and produced literally nothing until the 10-minute timeout
// fired at 08:00:29, while the immediately following attempt got a 429 response
// in five seconds. Pi's own retry backoff is delayMs: 2000, so a ten-minute gap
// is never Pi waiting politely — it is a request that will not come back.
//
// Waiting 10 minutes on a dead request wastes a worker slot; with five slots the
// run stopped closing beads entirely. Failing fast returns the slot for a retry
// that usually succeeds immediately. HYDRA_WORKER_ACTIVITY_TIMEOUT still
// overrides both.
func defaultActivityTimeout(piConfig PiConfig) time.Duration {
	if piConfig.RPCEnabled() {
		return 4 * time.Minute
	}
	return 10 * time.Minute
}

// executeWithPi executes the Pi agent CLI with activity-based timeout monitoring
func executeWithPi(
	bead types.Bead,
	piConfig PiConfig,
	mcpClient *mcp.Client,
	projectPath string,
	hindsightContext *agents.ContextEnrichment,
	extras promptExtras,
) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Success:   false,
		AgentType: "pi",
	}

	// Activity-based timeout: kill Pi after this long with NO output at all.
	// A hung/stalled Pi (dead LLM stream, wedged tool call) produces nothing,
	// while a working Pi streams reasoning/tool output and resets the timer
	// constantly — so this recycles stuck workers for a fast retry without
	// touching healthy ones. Lowered from 30m (which let stalls burn up to the
	// 15m maxTotalRuntime before Charon's blunter kill). Tunable via
	// HYDRA_WORKER_ACTIVITY_TIMEOUT (e.g. "8m", "15m").
	activityTimeout := defaultActivityTimeout(piConfig)
	if v := os.Getenv("HYDRA_WORKER_ACTIVITY_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			activityTimeout = d
		}
	}
	activityMonitor := NewActivityMonitor(activityTimeout)

	// Max total runtime: the lifetime cap (PRD Part 5) — monotonic, nothing
	// resets it, parked time excluded (parking is waiting, not running).
	// Default 2h; HYDRA_WORKER_LIFETIME_MINUTES overrides. The 4-minute
	// activity timeout remains the real hang detector; the registry
	// heartbeat is the daemon's.
	maxTotalRuntime := 2 * time.Hour
	if v := os.Getenv("HYDRA_WORKER_LIFETIME_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTotalRuntime = time.Duration(n) * time.Minute
		}
	}
	startTime := time.Now()

	// Build pi command
	cmd, piSessionID, piSessionDir := buildPiCommand(bead, piConfig, projectPath, hindsightContext)
	if piSessionID != "" && activeReporter != nil {
		activeReporter.SetSessionID(piSessionID)
	}
	if piSessionDir != "" {
		activityMonitor.SetSessionDir(piSessionDir)
	}

	log.Printf("🔧 Executing Pi: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))
	log.Printf("📁 Working directory: %s", cmd.Dir)
	if piSessionDir != "" {
		log.Printf("🗂️  Pi session directory: %s", piSessionDir)
	}
	log.Printf("⏱️  Activity timeout: %v (resets on any output)", activityTimeout)
	streamLog(mcpClient, "info", fmt.Sprintf("Executing Pi agent in %s with activity monitoring", cmd.Dir))

	// Create pipes for stdout and stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	defer stdoutPipe.Close()

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return result, fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	defer stderrPipe.Close()

	// Create stdin pipe for Pi text mode (plain text prompt via stdin)
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return result, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("failed to start pi: %w", err)
	}

	// Build the prompt message
	prompt := buildPiPrompt(bead, hindsightContext, extras)

	// turn is non-nil only in rpc mode, where it owns the protocol state and the
	// verdict. It stays nil in text mode so that path is byte-for-byte unchanged.
	var turn *rpcTurn

	if piConfig.RPCEnabled() {
		turn = newRPCTurn(piConfig.RPCAutoConfirm)
		promptLine, err := rpcPromptLine(prompt)
		if err != nil {
			return result, fmt.Errorf("failed to encode rpc prompt: %w", err)
		}
		if _, err := stdinPipe.Write(append(promptLine, '\n')); err != nil {
			return result, fmt.Errorf("failed to send rpc prompt to pi: %w", err)
		}
		// stdin deliberately stays OPEN. Replies to blocking UI asks travel back
		// over it, so closing it here would strand Pi waiting on a question
		// nobody can answer — the exact hang that made every earlier rpc attempt
		// look like a timeout.
		log.Printf("[Worker:rpc] prompt sent over rpc (autoConfirm=%v); awaiting agent_end", piConfig.RPCAutoConfirm)
	} else {
		// Send prompt as plain text via stdin
		if _, err := stdinPipe.Write([]byte(prompt + "\n")); err != nil {
			return result, fmt.Errorf("failed to send prompt to pi: %w", err)
		}
		// Close stdin to signal EOF — Pi starts processing when stdin closes
		stdinPipe.Close()
	}

	// Capture output with activity monitoring
	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup

	// Verdict-nudge state (rpc mode only): a turn that ends without a valid
	// HYDRA_VERDICT is re-prompted in the same live session instead of being
	// torn down. Capped; past the cap the turn closes and Pan sees the
	// nudge-capped worker in the registry.
	nudgeCount := 0

	// Park state (rpc mode only): Tier A provider-capacity failures park the
	// live session instead of killing it (PRD Part 2).
	park := newParkState(0)

	// Monitor stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := newPiScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stdoutBuf.WriteString(line + "\n")
			activityMonitor.RecordActivity()
			// In rpc mode every line is a protocol event: answer blocking UI
			// asks, accumulate assistant text, and detect the end of the turn.
			// Pi does not exit on its own once the turn is done, so finish()
			// tears it down — otherwise the worker would wait out the full
			// activity timeout on every bead.
			if turn != nil && turn.handleLine(line, stdinPipe) {
				// Failed turn: park on Tier A provider-capacity errors instead of
				// tearing the session down (PRD Part 2). Park cap reached or
				// non-parkable → the turn closes and the failure propagates.
				if turn.Failed() {
					if parkable, reason := decideParkClass(turn.ErrMsg()); parkable {
						if wait, ok := park.nextWait(reason); ok {
							parkSession(park, wait, reason, stdinPipe, turn,
								"The provider rejected the previous request for capacity reasons. It should accept requests again now — continue your task from where you stopped.")
							continue
						}
						log.Printf("[Worker:rpc] park cap reached (%v) — closing failed turn (%s)", park.totalParked(), reason)
					}
					turn.finish(cmdPid(cmd), stdinPipe)
					continue
				}
				v := parseVerdict(turn.Text())
				if v != nil {
					result.Verdict = v
				}
				if nudge, reason := decideTurnEnd(v, nudgeCount, verdictNudgeCap); nudge {
					nudgeCount++
					if activeReporter != nil {
						activeReporter.SetNudges(nudgeCount)
					}
					log.Printf("[Worker:rpc] turn ended without closure (%s) — nudging session %d/%d", reason, nudgeCount, verdictNudgeCap)
					if nudgeLine, err := rpcPromptLine(buildNudgePrompt(reason)); err == nil {
						if _, err := stdinPipe.Write(append(nudgeLine, '\n')); err != nil {
							log.Printf("[Worker:rpc] nudge write failed, finishing turn: %v", err)
							turn.finish(cmdPid(cmd), stdinPipe)
							continue
						}
						turn.reset()
						continue
					}
				}
				if v != nil && !v.Done && v.Summary != "" {
					log.Printf("[Worker:rpc] agent abandoned with summary: %s", truncateLog(v.Summary, 200))
				}
				turn.finish(cmdPid(cmd), stdinPipe)
			}
			// Also stream to MCP for real-time monitoring
			if mcpClient != nil {
				streamLog(mcpClient, "info", fmt.Sprintf("[pi-stdout] %s", line))
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("[Worker] pi stdout scanner stopped early: %v (activity monitor blind from here — a healthy pi may be watchdog-killed)", err)
		}
	}()

	// Monitor stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := newPiScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stderrBuf.WriteString(line + "\n")
			activityMonitor.RecordActivity()
			// Also stream to MCP for real-time monitoring
			if mcpClient != nil {
				streamLog(mcpClient, "warn", fmt.Sprintf("[pi-stderr] %s", line))
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("[Worker] pi stderr scanner stopped early: %v", err)
		}
	}()

	// Wait for completion or timeout
	done := make(chan error, 1)
	go func() {
		wg.Wait()
		done <- cmd.Wait()
	}()

	// Monitor for timeout
	timeoutTicker := time.NewTicker(10 * time.Second)
	defer timeoutTicker.Stop()

	var cmdErr error
monitorLoop:
	for {
		select {
		case cmdErr = <-done:
			break monitorLoop
		case <-timeoutTicker.C:
			// Parked workers are exempt from the activity timeout: parking
			// produces no output by design. Liveness of a parked worker is the
			// daemon's job (heartbeat + Pan), not this monitor's.
			if activityMonitor.IsTimedOut() && !park.isActive() {
				log.Printf("⏰ Activity timeout reached (no output for %v)", activityTimeout)
				streamLog(mcpClient, "warn", fmt.Sprintf("Activity timeout after %v of inactivity", activityTimeout))

				// Kill the entire process group
				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
					time.Sleep(2 * time.Second)
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}

				cmdErr = fmt.Errorf("activity timeout: no output for %v", activityTimeout)
				break monitorLoop
			}

			// Max total runtime check — kills Pi even if it produces sporadic
			// output. Parked time is excluded: parking is waiting, not running.
			if time.Since(startTime)-park.totalParked() > maxTotalRuntime {
				log.Printf("⏰ Max total runtime reached (%v). Forcing Pi termination.", maxTotalRuntime)
				streamLog(mcpClient, "warn", fmt.Sprintf("Max total runtime %v reached, terminating Pi", maxTotalRuntime))

				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
					time.Sleep(3 * time.Second)
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}

				cmdErr = fmt.Errorf("max total runtime exceeded: %v", maxTotalRuntime)
				break monitorLoop
			}
		}
	}

	// Process result
	result.ExitCode = 0
	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	// Store output
	result.Output = stdoutBuf.String()
	result.Error = stderrBuf.String()

	// Determine success. In rpc mode the exit code is meaningless: the worker
	// itself kills Pi after agent_end (Pi would otherwise sit waiting for
	// another prompt), so the process always dies by signal even on a perfect
	// run. The event stream is the only honest verdict — and it must be read
	// carefully, because a turn can reach agent_end and still carry a model
	// failure in its message (stopReason/errorMessage).
	if turn != nil && turn.Ended() {
		if txt := turn.Text(); strings.TrimSpace(txt) != "" {
			result.Output = txt
		}
		if s := turn.Stats(); s != nil {
			log.Printf("📊 rpc session tokens: input=%d output=%d cacheRead=%d total=%d",
				s.Input, s.Output, s.CacheRead, s.Total)
			reportTurnTokens(s)
		}
		if n := turn.Notifications(); n != "" {
			log.Printf("[Worker:rpc] extension UI notifications: %s", n)
		} else {
			log.Printf("[Worker:rpc] no extension UI notifications — extensions may not have loaded")
		}
		if turn.UIAnswered() > 0 {
			log.Printf("[Worker:rpc] answered %d blocking UI ask(s) during this turn", turn.UIAnswered())
		}
		if turn.Failed() {
			result.Success = false
			result.ExitCode = 1
			if msg := turn.ErrMsg(); msg != "" {
				result.Error = msg
			}
			log.Printf("❌ Pi rpc turn reached agent_end but the model call FAILED: %s", turn.ErrMsg())
		} else {
			result.Success = true
			result.ExitCode = 0
			log.Printf("✅ Pi rpc turn completed (agent_end, last activity %v ago)", activityMonitor.TimeSinceActivity())
		}
	} else if cmdErr == nil && result.ExitCode == 0 {
		result.Success = true
		log.Printf("✅ Pi completed successfully (last activity %v ago)", activityMonitor.TimeSinceActivity())
	} else {
		result.Success = false
		log.Printf("❌ Pi failed with exit code %d", result.ExitCode)
		// The watchdog's kill reason lives in cmdErr, not in pi's stderr —
		// surface it so classifyFailure can route it to an infra exit code.
		appendForcedKillReason(result, cmdErr)
		if stdoutStr := stdoutBuf.String(); stdoutStr != "" {
			log.Printf("📤 STDOUT:\n%s", stdoutStr)
		}
		if stderrStr := stderrBuf.String(); stderrStr != "" {
			log.Printf("📤 STDERR:\n%s", stderrStr)
		}
	}

	// Write output to temp file for durability and post-processing
	tempFile, err := writeExecutionOutputToFile(bead.ID.String(), result)
	if err != nil {
		log.Printf("[WARN] Failed to write execution output to file: %v", err)
	} else {
		log.Printf("📝 Execution output written to: %s", tempFile)

		// Schedule file cleanup after retention (optional - can keep for debugging)
		defer func() {
			// Keep file for 1 hour then clean up
			time.AfterFunc(1*time.Hour, func() {
				if err := os.Remove(tempFile); err == nil {
					log.Printf("🗑️  Cleaned up temp file: %s", tempFile)
				}
			})
		}()
	}

	return result, cmdErr
}

// writeExecutionOutputToFile writes the execution result to a temp file for durability
// and post-processing. Returns the file path or empty string on error.
func writeExecutionOutputToFile(beadID string, result *ExecutionResult) (string, error) {
	// Create temp directory if it doesn't exist
	tempDir := filepath.Join(os.TempDir(), "hydra-executions")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Create temp file with timestamp and bead ID
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s.json", beadID, timestamp)
	filepath := filepath.Join(tempDir, filename)

	// Build execution record
	record := map[string]interface{}{
		"bead_id":    beadID,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"success":    result.Success,
		"exit_code":  result.ExitCode,
		"duration":   result.Duration.Seconds(),
		"attempts":   result.Attempts,
		"agent_type": result.AgentType,
		"output":     result.Output,
		"error":      result.Error,
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal execution record: %w", err)
	}

	// Write to file atomically (write to temp, then rename)
	tempFile := filepath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tempFile, filepath); err != nil {
		os.Remove(tempFile) // Clean up on error
		return "", fmt.Errorf("failed to rename temp file: %w", err)
	}

	return filepath, nil
}

// readExecutionOutputFromFile reads an execution result from a temp file
func readExecutionOutputFromFile(filepath string) (*ExecutionResult, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var record map[string]interface{}
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal record: %w", err)
	}

	result := &ExecutionResult{}

	if success, ok := record["success"].(bool); ok {
		result.Success = success
	}
	if exitCode, ok := record["exit_code"].(float64); ok {
		result.ExitCode = int(exitCode)
	}
	if agentType, ok := record["agent_type"].(string); ok {
		result.AgentType = agentType
	}
	if output, ok := record["output"].(string); ok {
		result.Output = output
	}
	if errStr, ok := record["error"].(string); ok {
		result.Error = errStr
	}
	if attempts, ok := record["attempts"].(float64); ok {
		result.Attempts = int(attempts)
	}

	return result, nil
}

// buildPiCommand builds the Pi agent CLI command
func buildPiCommand(bead types.Bead, piConfig PiConfig, projectPath string, hindsightContext *agents.ContextEnrichment) (*exec.Cmd, string, string) {
	// Find pi binary
	piPath := os.Getenv("HYDRA_PI_PATH")
	if piPath == "" {
		piPath = "pi"
	}

	// Use project path if provided
	path := projectPath
	if path == "" {
		path = bead.Project
	}

	// Named pi session: deterministic per bead so a respawned worker (or a
	// daemon restart) can resume the same context instead of starting from
	// zero. The session directory lives under $HYDRA_HOME/sessions.
	sessionID, sessionDir, err := buildPiSession(bead, os.Getenv("HYDRA_HOME"))
	if err != nil {
		log.Printf("[Worker] Warning: could not build pi session: %v", err)
		sessionID, sessionDir = "", ""
	}
	if sessionDir != "" {
		if err := os.MkdirAll(sessionDir, 0750); err != nil {
			log.Printf("[Worker] Warning: could not create session dir %s: %v", sessionDir, err)
			sessionID, sessionDir = "", ""
		}
	}

	// Build base args — default text mode reads prompt from stdin.
	// --offline skips Pi's startup provider-list network fetch (which flakes
	// under concurrent worker launches → spurious "Unknown provider") and uses
	// the local models.json cache. Passed as a flag (not just env) so it can't
	// be lost in env propagation.
	args := []string{
		"--offline",
	}
	if sessionID != "" && sessionDir != "" {
		// --session-id (not --session): pi exits 1 with "No session found
		// matching" when --session names a session that does not exist yet —
		// i.e. every fresh bead. --session-id creates it if missing.
		args = append(args, "--session-id", sessionID, "--session-dir", sessionDir)
	}

	// RPC mode is what makes Pi initialise its extensions (rtk command
	// rewriting, context-mode, caveman). The piped text mode initialises none
	// of them, so every package declared in the worker's settings.json was
	// installed but dormant. Verified in tools/pi-rpc.
	if piConfig.RPCEnabled() {
		args = append(args, "--mode", "rpc")
		// Load exactly the allowlisted extensions. Only attempted in rpc mode:
		// text mode initialises no extension host, so these flags would do
		// nothing but lengthen the command line.
		if extArgs := extensionArgs(resolveExtensionPaths(resolvePiAgentDir(piConfig.ConfigDir), piConfig.Extensions)); len(extArgs) > 0 {
			args = append(args, extArgs...)
			log.Printf("[Worker:rpc] loading %d allowlisted extension(s): %s",
				(len(extArgs)-1)/2, strings.Join(piConfig.Extensions, ", "))
		}
	}

	// Add provider
	if piConfig.Provider != "" {
		args = append(args, "--provider", piConfig.Provider)
	}

	// Add model
	if piConfig.Model != "" {
		args = append(args, "--model", piConfig.Model)
	}

	// Build skill flags if configDir is set
	if piConfig.ConfigDir != "" {
		skillResult, err := pi.BuildSkillFlagsForProject(piConfig.ConfigDir, projectPath)
		if err != nil {
			log.Printf("[Worker] Warning: failed to build skill flags: %v", err)
		} else {
			args = append(args, skillResult.Flags...)
			log.Printf("[Worker] Added %d skill flags", len(skillResult.Flags)/2)
		}
	}

	// Create command
	cmd := exec.Command(piPath, args...)
	cmd.Dir = path

	// Create new process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Prepare environment
	envVars := os.Environ()

	// Force Pi offline at the launch site: skip its startup provider-list
	// fetch (which flakes under concurrent worker launches, causing spurious
	// "Unknown provider") and use the local models.json cache. Set explicitly
	// here rather than relying on inheritance so it always reaches Pi.
	envVars = append(envVars, "PI_OFFLINE=1")

	// Set PI_CODING_AGENT_DIR if configDir is set. resolvePiAgentDir picks the
	// directory that actually holds the package set rather than blindly appending
	// "agent" — appending it on the VPS layout pointed Pi at an empty directory,
	// so every worker ran with no extensions loaded at all.
	if piConfig.ConfigDir != "" {
		if agentDir := resolvePiAgentDir(piConfig.ConfigDir); agentDir != "" {
			envVars = append(envVars, fmt.Sprintf("PI_CODING_AGENT_DIR=%s", agentDir))
		}
	}

	// Set PI_CODING_AGENT_SESSION_DIR if we have a stable session dir so that
	// sub-processes and the park/resume path agree on where the session lives.
	if sessionDir != "" {
		envVars = append(envVars, fmt.Sprintf("PI_CODING_AGENT_SESSION_DIR=%s", sessionDir))
	}

	// Set API key from environment if configured
	if piConfig.APIKeyName != "" {
		apiKey := os.Getenv(piConfig.APIKeyName)
		if apiKey != "" {
			envVars = append(envVars, fmt.Sprintf("%s=%s", piConfig.APIKeyName, apiKey))
		}
	}

	// Set bead metadata
	if bead.Title != "" {
		envVars = append(envVars, fmt.Sprintf("HYDRA_BEAD_TITLE=%s", bead.Title))
	}
	if bead.Description != "" {
		envVars = append(envVars, fmt.Sprintf("HYDRA_BEAD_DESCRIPTION=%s", bead.Description))
	}

	// Set agent type
	envVars = append(envVars, fmt.Sprintf("HYDRA_AGENT_TYPE=%s", bead.Type))
	envVars = append(envVars, fmt.Sprintf("HYDRA_BEAD_TYPE=%s", bead.Type))

	cmd.Env = envVars

	// Report the session id to the daemon so a replacement worker can resume it.
	if sessionID != "" && activeReporter != nil {
		activeReporter.SetSessionID(sessionID)
	}

	return cmd, sessionID, sessionDir
}

// buildPiSession returns a deterministic session id and directory for a bead.
// The id is safe to use as a file name and the directory is under HYDRA_HOME
// so it survives the worker process and can be reused after a daemon restart.
func buildPiSession(bead types.Bead, hydraHome string) (string, string, error) {
	if hydraHome == "" {
		return "", "", fmt.Errorf("HYDRA_HOME not set")
	}
	sessionID := sanitizeSessionID(string(bead.ID))
	if sessionID == "" {
		sessionID = "bead"
	}
	sessionDir := filepath.Join(hydraHome, "sessions", sessionID)
	return sessionID, sessionDir, nil
}

func sanitizeSessionID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// buildPiPrompt builds the prompt message for Pi agent
// phaseGuidance returns phase-specific working doctrine for a bead, keyed on
// the TDD-phase title prefix task-breakdown emits (RED:/GREEN:/REFACTOR:) and
// on fix-type beads. Empty for phases with no special discipline.
func phaseGuidance(bead types.Bead) string {
	title := strings.ToUpper(strings.TrimSpace(bead.Title))
	switch {
	case strings.HasPrefix(title, "RED:"):
		return "RED-phase discipline:\n" +
			"- Write tests that pin BEHAVIOR from the acceptance criteria, never implementation details.\n" +
			"- Run the tests and verify each fails for the RIGHT reason — the missing feature — not a compile error or typo. Quote the failure output.\n" +
			"- Do NOT implement the feature in this task."
	case strings.HasPrefix(title, "GREEN:"):
		return "GREEN-phase discipline:\n" +
			"- You are the implementer: EDIT the target files now. Ending this task with only a plan or analysis is a failure — the close gate rejects tasks that changed nothing.\n" +
			"- Implement the MINIMUM that makes the failing tests pass. No speculative abstractions, no extra options, no gold-plating.\n" +
			"- Run the tests and quote the passing output.\n" +
			"- If a test itself looks wrong, say so explicitly in your summary instead of contorting the implementation to satisfy it."
	case strings.HasPrefix(title, "REFACTOR:"):
		return "REFACTOR-phase discipline:\n" +
			"- Behavior must not change. Run the tests BEFORE and AFTER your change; quote both outputs.\n" +
			"- Improve names, structure, and duplication only.\n" +
			"- If you find a real bug, do NOT fix it here: record it with `cm add` and finish the refactor unchanged."
	case strings.HasPrefix(title, "FIX:") || strings.EqualFold(bead.Type, "fix"):
		return "FIX discipline — root-cause protocol, follow strictly:\n" +
			"1. REPRODUCE: run the failing command/test first; capture the exact error text.\n" +
			"2. ISOLATE: form ONE hypothesis; design the smallest experiment that can disprove it (a single command, a tiny probe). Do not fix anything yet.\n" +
			"3. VERIFY the hypothesis with that experiment. Wrong? Form the next hypothesis from the new evidence — never stack two unverified guesses.\n" +
			"4. FIX the proven root cause, not the symptom. Silencing an error, skipping a test, or widening a timeout is a symptom fix — reject it unless the evidence shows it IS the root cause.\n" +
			"5. PROVE: re-run the originally failing command and quote its passing output.\n" +
			"Any prior-failure text in this prompt is evidence for step 1 — read it first."
	}
	return ""
}

func buildPiPrompt(bead types.Bead, hindsightContext *agents.ContextEnrichment, extras promptExtras) string {
	var promptParts []string

	// Add bead type and title
	if bead.Title != "" {
		promptParts = append(promptParts, fmt.Sprintf("%s: %s", bead.Type, bead.Title))
	} else {
		promptParts = append(promptParts, bead.Type)
	}

	// Add description
	if bead.Description != "" {
		promptParts = append(promptParts, bead.Description)
	}

	// Per-phase working doctrine: each TDD phase (and fix beads) gets the
	// discipline that phase actually needs, instead of one generic prompt.
	if g := phaseGuidance(bead); g != "" {
		promptParts = append(promptParts, g)
	}

	// Anti-consultant guard: implementation-type beads must produce file
	// edits. Observed failure mode (2026-07-17): doctrine-tuned workers
	// honestly delivered PLANS with "NOT VERIFIED" labels and zero changes —
	// every close was then rejected as empty-diff. State the contract.
	switch strings.ToLower(strings.TrimSpace(bead.Type)) {
	case "fr", "fix", "implement", "green", "refactor", "task", "us-heavy":
		promptParts = append(promptParts,
			"DELIVERABLE CONTRACT: this task is complete only when the required file edits exist on disk and verification commands ran. A plan or analysis without edits will be rejected as an empty diff.")
	}

	// Turn economy (session forensics 2026-08-17): every turn re-bills the
	// full transcript (16K -> 244K over one bead = 25M tokens, 24.8M cache
	// reads). The model defaults to one tool call per turn (measured 1.00
	// calls/turn on workers, 1.02 on interactive sessions) — this guidance
	// pushes against that default. It is a hedge, not a fix: the structural
	// levers are early compaction (project .pi/settings.json) and the
	// turn-level 403 breaker.
	promptParts = append(promptParts,
		"TURN ECONOMY: every turn re-sends the entire conversation to the model — turns are the cost multiplier.\n" +
			"- Batch independent tool calls in the same message (multiple reads, one search+read pair, etc.).\n" +
			"- Prefer one comprehensive command over a probe sequence (e.g. one script that gathers everything over five separate calls).\n" +
			"- Do NOT re-run the full test suite between micro-edits: run the narrowest failing test until it passes, then the full suite ONCE at the end.\n" +
			"- Read files at the offsets you need instead of re-reading whole files.")

	// Turn horizon (2026-08-22): the economy block says turns are expensive
	// but implies infinite runway; 200-turn grinds (20M tokens) resulted. An
	// explicit budget makes the model pace itself and converts wandering
	// into an early honest unmet verdict instead of a grind to the wall.
	promptParts = append(promptParts,
		"TURN BUDGET: plan for ~40 turns total. Track your own count. If you pass 40 without the narrow test green: stop exploring, commit whatever progress exists, and emit HYDRA_VERDICT with done:false and your findings — an honest unmet verdict is a good outcome; grinding without convergence is not. When running tests, pipe output through tail (`... 2>&1 | tail -30`) — full test output floods the context every turn.")

	// Daemon-owned gates (FR-002): when the project delegates verification to
	// the daemon, the prompt redirects the agent away from suite babysitting.
	if os.Getenv("HYDRA_DAEMON_GATES") == "1" {
		promptParts = append(promptParts,
			"VERIFICATION OWNERSHIP: the daemon runs the project's test/lint/build gates after you finish and re-verifies on every completion. Do NOT run the full test suite or coverage commands yourself — fix code, run only the narrowest test that proves your change, and end your turn. If a previous gate failed, its parsed summary and raw log path are in this bead's description; start from that evidence.")
	}

	// Closure protocol (PRD worker-park-and-resume Part 3): the turn does not
	// end when the agent stops talking — it ends with a machine-readable
	// verdict. Without one the worker re-prompts the live session.
	promptParts = append(promptParts,
		`CLOSURE PROTOCOL: end your final message with exactly one verdict block.
When done:
HYDRA_VERDICT: {"done": true, "ac_met": [...], "ac_unmet": [], "files_changed": [...], "verification": [{"cmd": "...", "result": "..."}]}
When you cannot complete it:
HYDRA_VERDICT: {"done": false, "summary": "what was done and what remains"}
A turn ending without this block is treated as unfinished and you will be re-prompted.`)

	// Add target files if available
	targetFiles := extractTargetFiles(bead)
	if len(targetFiles) > 0 {
		promptParts = append(promptParts, fmt.Sprintf("Files to modify: %v", targetFiles))
	}

	// Add Hindsight context if available. Memories arrive server-ranked and
	// already truncated to the configured limit; the recall API sends no
	// per-memory score, so none is shown (a phantom 0.00 was displayed
	// before). Memory IDs are internal noise — content only.
	if hindsightContext != nil && len(hindsightContext.Memories) > 0 {
		promptParts = append(promptParts, "\nRelevant past executions (most relevant first):")
		for _, mem := range hindsightContext.Memories {
			promptParts = append(promptParts, "- "+truncateString(mem.Content, 200))
		}
	}

	// Distilled institutional rules, injected deterministically (the agent is
	// also told to query CASS itself, but that instruction is skippable).
	if len(extras.CassBullets) > 0 {
		promptParts = append(promptParts, "\nProven rules from this codebase's playbook — follow them:")
		for _, b := range extras.CassBullets {
			promptParts = append(promptParts, "- "+truncateString(b, 300))
		}
	}

	// Reflexion: tell the agent what the last attempt did wrong so it
	// diagnoses instead of repeating the identical approach.
	if extras.PrevFailure != "" {
		promptParts = append(promptParts,
			"\nPREVIOUS ATTEMPT FAILED: "+truncateString(extras.PrevFailure, 600)+
				"\nDo not repeat the same approach. Diagnose the failure above first, then fix the root cause.")
	}

	return strings.Join(promptParts, "\n\n")
}

// queryHindsightForContext queries Hindsight for relevant context before agent execution
// Returns the context enrichment data to be included in the prompt
func queryHindsightForContext(
	bead types.Bead,
	config WorkerConfig,
	mcpClient *mcp.Client,
) *agents.ContextEnrichment {
	// Skip if Hindsight is not enabled
	if !config.HindsightClient.Enabled {
		log.Println("ℹ️ Hindsight not enabled, skipping context query")
		return nil
	}

	log.Printf("🔍 Querying Hindsight for context (bead: %s)...", bead.ID)
	streamLog(mcpClient, "info", "Querying Hindsight for relevant context")

	// Create Hindsight client with config
	hindsightClient := hindsight.NewClient(
		hindsight.WithBaseURL(config.HindsightClient.BaseURL),
		hindsight.WithTimeout(config.HindsightClient.Timeout),
		hindsight.WithDefaultLimit(config.HindsightClient.Limit),
		hindsight.WithMaxTokens(config.HindsightClient.MaxTokens),
		hindsight.WithEnabled(true),
	)

	// Create context processor
	ctxProcessorConfig := agents.DefaultContextProcessorConfig()
	ctxProcessorConfig.Enabled = true
	ctxProcessor := agents.NewContextProcessor(hindsightClient, ctxProcessorConfig, nil)

	// Query Hindsight for context
	ctx := context.Background()
	enrichment, err := ctxProcessor.EnrichBeadContext(ctx, bead)
	if err != nil {
		log.Printf("⚠️ Failed to query Hindsight for context: %v", err)
		streamLog(mcpClient, "warn", fmt.Sprintf("Hindsight context query failed: %v", err))
		return nil
	}

	// Log the results
	if len(enrichment.Memories) > 0 {
		log.Printf("✅ Retrieved %d relevant memories from Hindsight (query time: %v)",
			len(enrichment.Memories), enrichment.QueryTime)
		streamLog(mcpClient, "info", fmt.Sprintf("Retrieved %d relevant memories from Hindsight", len(enrichment.Memories)))

		// Log each memory (rank order; the recall API sends no scores)
		for i, mem := range enrichment.Memories {
			log.Printf("   Memory %d: %s - %s",
				i+1, mem.ID, truncateString(mem.Content, 100))
		}
	} else {
		log.Println("ℹ️ No relevant memories found in Hindsight")
		streamLog(mcpClient, "info", "No relevant memories found in Hindsight")
	}

	return enrichment
}

// truncateString truncates a string to the specified length with ellipsis
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// recordExecutionResult records the execution outcome to hindsight memory
func recordExecutionResult(
	bead types.Bead,
	result *ExecutionResult,
	mcpClient *mcp.Client,
	config WorkerConfig,
) {
	if result == nil {
		log.Println("[WARN] Cannot record nil result")
		return
	}

	// Initialize hindsight client with config from environment
	var hindsightClient *hindsight.Client
	if config.HindsightClient.Enabled {
		hindsightClient = hindsight.NewClient(
			hindsight.WithBaseURL(config.HindsightClient.BaseURL),
			hindsight.WithTimeout(config.HindsightClient.Timeout),
			hindsight.WithDefaultLimit(config.HindsightClient.Limit),
			hindsight.WithMaxTokens(config.HindsightClient.MaxTokens),
			hindsight.WithEnabled(true),
		)
		log.Printf("[Hindsight] Client initialized (baseURL: %s, limit: %d, maxTokens: %d)",
			config.HindsightClient.BaseURL, config.HindsightClient.Limit, config.HindsightClient.MaxTokens)
	} else {
		log.Println("ℹ️ Hindsight not enabled, skipping result recording")
		return
	}

	// Create execution memory with structured data
	memory := hindsight.NewExecutionMemory(bead, agentID).
		WithAgentType(result.AgentType).
		WithOutcome(
			result.Success,
			result.ExitCode,
			buildRetainContent(result),
			result.Error,
			result.Duration,
			result.Attempts,
		)

	// Add target files from bead metadata
	targetFiles := extractTargetFiles(bead)
	if len(targetFiles) > 0 {
		memory.Files.TargetFiles = targetFiles
	}

	// Retain the execution memory in hindsight
	retainResult, err := hindsightClient.Retain(memory)
	if err != nil {
		log.Printf("[WARN] Failed to retain execution memory: %v", err)
		streamLog(mcpClient, "warn", fmt.Sprintf("Failed to retain execution memory: %v", err))
	} else if !retainResult.Success {
		log.Printf("[WARN] Hindsight retention failed: %s", retainResult.Error)
		streamLog(mcpClient, "warn", fmt.Sprintf("Hindsight retention failed: %s", retainResult.Error))
	} else {
		log.Printf("✅ Retained execution memory in Hindsight (memory_id: %s)", retainResult.MemoryID)
		streamLog(mcpClient, "info", fmt.Sprintf("Retained execution memory in Hindsight"))
	}
}

// recordExecutionResultSync is a synchronous version that doesn't require MCP
// It blocks until retention completes or fails
func recordExecutionResultSync(
	bead types.Bead,
	result *ExecutionResult,
	config WorkerConfig,
) error {
	if result == nil {
		return fmt.Errorf("cannot record nil result")
	}

	if !config.HindsightClient.Enabled {
		log.Println("ℹ️ Hindsight not enabled, skipping result recording")
		return nil
	}

	// Initialize hindsight client with config from environment
	hindsightClient := hindsight.NewClient(
		hindsight.WithBaseURL(config.HindsightClient.BaseURL),
		hindsight.WithTimeout(config.HindsightClient.Timeout),
		hindsight.WithDefaultLimit(config.HindsightClient.Limit),
		hindsight.WithMaxTokens(config.HindsightClient.MaxTokens),
		hindsight.WithEnabled(true),
	)
	log.Printf("[Hindsight] Client initialized for sync retention (baseURL: %s)", config.HindsightClient.BaseURL)

	// Create execution memory with structured data
	memory := hindsight.NewExecutionMemory(bead, agentID).
		WithAgentType(result.AgentType).
		WithOutcome(
			result.Success,
			result.ExitCode,
			buildRetainContent(result),
			result.Error,
			result.Duration,
			result.Attempts,
		)

	// Add target files from bead metadata
	targetFiles := extractTargetFiles(bead)
	if len(targetFiles) > 0 {
		memory.Files.TargetFiles = targetFiles
	}

	// Retain the execution memory in hindsight
	retainResult, err := hindsightClient.Retain(memory)
	if err != nil {
		return fmt.Errorf("failed to retain execution memory: %w", err)
	}

	if !retainResult.Success {
		return fmt.Errorf("hindsight retention failed: %s", retainResult.Error)
	}

	log.Printf("✅ Retained execution memory in Hindsight (memory_id: %s)", retainResult.MemoryID)
	return nil
}

// buildSuccessPattern builds a success pattern string from the execution result
func buildSuccessPattern(bead types.Bead, result *ExecutionResult) string {
	if bead.Title == "" {
		return ""
	}

	var parts []string

	// Base pattern from bead title
	parts = append(parts, bead.Title)

	// Add agent type
	if result.AgentType != "" {
		parts = append(parts, fmt.Sprintf("[Agent: %s]", result.AgentType))
	}

	// Add result status
	if result.Success {
		parts = append(parts, "→ SUCCESS")
	} else {
		parts = append(parts, "→ FAILURE")
	}

	// Add key output summary if available
	if result.Output != "" {
		// Truncate output to first line or 100 chars
		summary := result.Output
		if idx := strings.Index(summary, "\n"); idx > 0 {
			summary = summary[:idx]
		}
		if len(summary) > 100 {
			summary = summary[:100] + "..."
		}
		if summary != "" {
			parts = append(parts, fmt.Sprintf("Output: %s", summary))
		}
	}

	// Add error summary if failed
	if !result.Success && result.Error != "" {
		summary := result.Error
		if idx := strings.Index(summary, "\n"); idx > 0 {
			summary = summary[:idx]
		}
		if len(summary) > 100 {
			summary = summary[:100] + "..."
		}
		if summary != "" {
			parts = append(parts, fmt.Sprintf("Error: %s", summary))
		}
	}

	return strings.Join(parts, " | ")
}

// notifyTaskCompleteWithResult sends task completion with full execution result via MCP
func notifyTaskCompleteWithResult(
	client *mcp.Client,
	bead types.Bead,
	result *ExecutionResult,
	status string,
	duration time.Duration,
	exitCode int,
	agentID string,
) {
	if client == nil {
		return
	}

	// Validate status
	if !mcp.IsValidStatus(status) {
		status = "failure"
	}

	// Build comprehensive payload with all execution data
	payload := map[string]interface{}{
		"bead_id":    bead.ID.String(),
		"bead_title": bead.Title,
		"bead_type":  bead.Type,
		"project":    bead.Project,
		"repo":       bead.Repo,
		"agent_id":   agentID,
		"status":     status,
		"duration":   duration.Seconds(),
		"exit_code":  exitCode,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}

	// Add execution result details if available
	if result != nil {
		payload["success"] = result.Success
		payload["attempts"] = result.Attempts
		payload["agent_type"] = result.AgentType
		if result.Output != "" {
			payload["output"] = result.Output
		}
		if result.Error != "" {
			payload["error"] = result.Error
		}
	}

	// Send to overseer
	err := client.SendMessage(mcp.MsgTypeTaskComplete, "overseer", payload)
	if err != nil {
		log.Printf("[WARN] Failed to send task completion with result: %v", err)
	} else {
		log.Printf("✅ Sent execution result to overseer for batch retention")
	}

	// Also broadcast to all agents (without full output to reduce noise)
	broadcastPayload := map[string]interface{}{
		"bead_id":   bead.ID.String(),
		"agent_id":  agentID,
		"status":    status,
		"duration":  duration.Seconds(),
		"exit_code": exitCode,
	}

	broadcastErr := client.Broadcast(mcp.MsgTypeTaskComplete, broadcastPayload)
	if broadcastErr != nil {
		log.Printf("[WARN] Failed to broadcast task completion: %v", broadcastErr)
	}
}

// notifyTaskComplete sends task completion notification via MCP
func notifyTaskComplete(client *mcp.Client, bead types.Bead, status string, duration time.Duration, exitCode int) {
	if client == nil {
		return
	}

	// Validate status
	if !mcp.IsValidStatus(status) {
		status = "failure"
	}

	// Send via message API to overseer
	err := client.SendMessage(mcp.MsgTypeTaskComplete, "overseer", map[string]interface{}{
		"bead_id":   bead.ID.String(),
		"status":    status,
		"duration":  duration.Seconds(),
		"exit_code": exitCode,
	})
	if err != nil {
		log.Printf("[WARN] Failed to send task completion message: %v", err)
	}

	// Also broadcast to all agents
	broadcastErr := client.Broadcast(mcp.MsgTypeTaskComplete, map[string]interface{}{
		"bead_id":   bead.ID.String(),
		"agent_id":  agentID,
		"status":    status,
		"duration":  duration.Seconds(),
		"exit_code": exitCode,
	})
	if broadcastErr != nil {
		log.Printf("[WARN] Failed to broadcast task completion: %v", broadcastErr)
	}
}

// streamLog sends a log message via MCP
func streamLog(client *mcp.Client, level, message string) {
	if client == nil {
		return
	}

	// Validate log level
	if !mcp.IsValidLogLevel(level) {
		level = "info"
	}

	if err := client.SendLog(level, message); err != nil {
		// Silent fail - logging shouldn't break execution
		log.Printf("[DEBUG] Failed to stream log: %v", err)
	}
}

// closeBeadFromWorker closes the bead using the beads CLI directly from the worker
// This makes the worker self-contained and resilient to overseer failures
func closeBeadFromWorker(bead types.Bead, projectPath, repoName, reason string) error {
	// Daemon-owned gates (FR-002): closure belongs to the daemon, after gates
	// pass. A worker self-close skips the entire gate loop — first dogfood run
	// (hydra-5cl, 2026-08-20) bypassed every gate this way.
	if os.Getenv("HYDRA_DAEMON_GATES") == "1" {
		log.Printf("🔒 Daemon-owned gates: worker does not close %s; the daemon closes after gates pass", bead.ID)
		return nil
	}

	// Use the beads CLI to close the bead
	// Command: br close <bead-id> -m <reason>

	beadID := bead.ID.String()

	log.Printf("🔒 Closing bead from worker: %s (reason: %s)", beadID, reason)
	log.Printf("   Project: %s, Path: %s, Repo: %s", bead.Project, projectPath, repoName)

	// First, try to use the beads client directly if we can get the project path
	if projectPath == "" {
		// Try to construct path from project name
		projectPath = filepath.Join(os.Getenv("HOME"), "source-code", bead.Project)
		if _, err := os.Stat(projectPath); os.IsNotExist(err) {
			// Try /tmp as fallback
			projectPath = filepath.Join("/tmp", bead.Project)
		}
		log.Printf("   Using fallback project path: %s", projectPath)
	}

	// Check if path exists
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		log.Printf("   [WARN] Project path does not exist: %s", projectPath)
		// Don't fail - the overseer will handle this asynchronously
		return fmt.Errorf("project path not found: %s", projectPath)
	}

	// Skip if the directory is not actually a beads workspace. This can happen
	// for testbed/synthetic beads or when the worktree has already been cleaned
	// up. Invoking br close in that directory produces the noisy
	// "Beads not initialized" exit-2 warning.
	if !beads.IsWorkspace(projectPath) {
		log.Printf("   [DEBUG] Not a beads workspace, skipping br close: %s", projectPath)
		return nil
	}

	// Try to close using br CLI directly
	// The br CLI needs to be run from the project directory
	cmd := exec.Command("br", "close", beadID, "-r", reason)
	cmd.Dir = projectPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("   [WARN] Failed to close bead via br CLI: %v", err)
		log.Printf("   Output: %s", string(output))
		return fmt.Errorf("br close failed: %w", err)
	}

	log.Printf("   Successfully closed bead via br CLI")
	return nil
}

// classifyFailure examines the execution result and classifies it into a
// specific exit code.  This lets hydra distinguish infrastructure failures
// (rate limits, timeouts, etc.) from code failures (stubs, build errors, etc.)
// so that fix beads are only created for real work gaps.
//
// Exit codes:
//
//	0 – success
//	1 – generic / unknown code failure
//	2 – rate-limit / API quota exceeded
//	3 – timeout / deadline exceeded
//	4 – infrastructure (network, daemon lock, permissions, etc.)
//	5 – stub / placeholder detected
//	6 – quota / billing limit exhausted (long cooldown, not a failover)
// appendForcedKillReason surfaces the worker watchdog's kill reason into
// result.Error. The reason (activity timeout, max total runtime) is built in
// the monitor loop as a plain error and never reaches pi's stderr — without
// this, classifyFailure saw only the stalled stream's stderr and the death
// classified as exit -1/1, a bead fault, spawning fix beads that died the same
// way (hydra-code-x77, 2026-08-07). Real process exits (exec.ExitError) are
// skipped: their stderr already carries the cause.
func appendForcedKillReason(result *ExecutionResult, cmdErr error) {
	if cmdErr == nil {
		return
	}
	var exitErr *exec.ExitError
	if errors.As(cmdErr, &exitErr) {
		return
	}
	if result.Error != "" {
		result.Error += "\n"
	}
	result.Error += cmdErr.Error()
}

func classifyFailure(result *ExecutionResult) int {
	if result == nil || result.Success {
		return 0
	}
	if result.Error == "" {
		return 1
	}
	err := strings.ToLower(result.Error)
	switch {
	// Concurrency ceiling, checked BEFORE quota because Kimi reports both as
	// HTTP 403 / access_terminated_error. Exit 7 routes it to the governor
	// (thin the fleet) instead of the breaker (freeze the fleet for 30m).
	case isConcurrencyLimit(err):
		return 7
	// Quota/billing exhaustion gets its own code so the daemon can apply a long
	// cooldown instead of a rate-limit failover: a 429 clears in seconds, a
	// billing limit clears when the cycle rolls over.
	case isQuotaError(err):
		return 6
	// Transient provider capacity, checked before the rate-limit markers because
	// the payload contains both ("429" + rate_limit_error + "overloaded"). Exit 7
	// is retryable infrastructure but must NOT trip the provider circuit: the
	// remedy is a short retry, not a fifteen-minute fleet freeze.
	case isTransientOverload(err):
		return 7
	case strings.Contains(err, "rate_limit") ||
		strings.Contains(err, "token plan") ||
		strings.Contains(err, "429"):
		return 2
	case strings.Contains(err, "timeout") ||
		strings.Contains(err, "deadline exceeded") ||
		strings.Contains(err, "max total runtime"):
		return 3
	case strings.Contains(err, "stub") ||
		strings.Contains(err, "placeholder") ||
		strings.Contains(err, "todo") ||
		strings.Contains(err, "fixme"):
		return 5
	case strings.Contains(err, "connection refused") ||
		strings.Contains(err, "connection reset") ||
		strings.Contains(err, "i/o error") ||
		strings.Contains(err, "permission denied") ||
		strings.Contains(err, "exit status 8") ||
		strings.Contains(err, "bead get failed") ||
		strings.Contains(err, "no such host") ||
		strings.Contains(err, "tls handshake"):
		return 4
	}
	return 1
}

// checkWorkComplete verifies that a bead's work is actually done by checking:
// 1. Target files exist and were modified during this session
// 2. Tests pass for the bead's scope (if test command is available)
// Returns true if the work appears complete even if Pi exited with error.
func checkWorkComplete(bead types.Bead, projectPath string) bool {
	// 1. Check if target files exist and have content
	targetFiles := extractTargetFiles(bead)
	if len(targetFiles) == 0 {
		// No target files specified — can't verify completion
		return false
	}

	allFilesExist := true
	allFilesHaveContent := true
	for _, relPath := range targetFiles {
		absPath := filepath.Join(projectPath, relPath)
		info, err := os.Stat(absPath)
		if err != nil {
			log.Printf("   [checkWorkComplete] Target file missing: %s", relPath)
			allFilesExist = false
			continue
		}
		if info.Size() == 0 {
			log.Printf("   [checkWorkComplete] Target file empty: %s", relPath)
			allFilesHaveContent = false
		}
	}

	if !allFilesExist {
		return false
	}
	if !allFilesHaveContent {
		return false
	}

	log.Printf("   [checkWorkComplete] All %d target files exist with content", len(targetFiles))

	// 2. Try to run tests for the affected packages
	// Derive package paths from target files (e.g., internal/campaigns/handler.go → ./internal/campaigns)
	pkgSet := make(map[string]bool)
	for _, relPath := range targetFiles {
		dir := filepath.Dir(relPath)
		// Only test Go packages
		if strings.HasSuffix(dir, ".go") {
			dir = filepath.Dir(dir)
		}
		// Skip if not under a recognized Go package root
		if strings.HasPrefix(dir, "internal/") || strings.HasPrefix(dir, "cmd/") || strings.HasPrefix(dir, "pkg/") {
			pkgSet["./"+dir] = true
		}
	}

	if len(pkgSet) == 0 {
		// No testable packages — assume work is complete if files exist
		return true
	}

	// Run tests for each package
	allTestsPass := true
	for pkg := range pkgSet {
		log.Printf("   [checkWorkComplete] Running tests for %s", pkg)
		cmd := exec.Command("go", "test", pkg)
		cmd.Dir = projectPath
		cmd.Env = os.Environ()
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("   [checkWorkComplete] Tests FAILED for %s: %v", pkg, err)
			log.Printf("   [checkWorkComplete] Output: %s", string(output))
			allTestsPass = false
		} else {
			log.Printf("   [checkWorkComplete] Tests PASSED for %s", pkg)
		}
	}

	return allTestsPass
}

// reportTurnTokens forwards pi's session token stats to the registry.
// Absolute, not incremental: get_session_stats is cumulative for the named
// session, and TokensSoFar is the registry's running total. Without this the
// registry always showed tokens_so_far = 0 (pi emits usage on every message;
// the worker parsed it into turn stats and then only logged it), which made
// Pan's zero-token stuck heuristics fire on healthy workers.
func reportTurnTokens(s *piagents.SessionStats) {
	if s == nil || activeReporter == nil {
		return
	}
	activeReporter.SetTokens(int64(s.Total))
}
