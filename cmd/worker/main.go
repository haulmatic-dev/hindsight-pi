package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/internal/pi"
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
	Pi              PiConfig
	MCP             struct {
		Enabled bool
		URL     string
	}
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
}

// HindsightClientConfig contains configuration for the Hindsight client
type HindsightClientConfig struct {
	Enabled  bool
	BaseURL  string
	Timeout  time.Duration
	Limit    int
	MinScore float64
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
	Success      bool
	ExitCode     int
	Output       string
	Error        string
	Duration     time.Duration
	Attempts     int
	AgentType    string
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
}

// NewActivityMonitor creates a new activity monitor
func NewActivityMonitor(timeout time.Duration) *ActivityMonitor {
	return &ActivityMonitor{
		lastActivity: time.Now(),
		timeout:      timeout,
	}
}

// RecordActivity records that activity occurred
func (am *ActivityMonitor) RecordActivity() {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.lastActivity = time.Now()
}

// IsTimedOut checks if timeout has elapsed since last activity
func (am *ActivityMonitor) IsTimedOut() bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return time.Since(am.lastActivity) > am.timeout
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
	result.Duration = duration

	// Determine status
	status := "success"
	exitCode := 0
	if err != nil || !result.Success {
		status = "failure"
		exitCode = 1
		if result != nil {
			exitCode = result.ExitCode
		}
	}

	// STEP 4: Send execution results to overseer for batch retention
	// The overseer's batch retention service will handle Hindsight storage efficiently
	log.Printf("📝 Sending execution results to overseer for batch retention...")
	streamLog(mcpClient, "info", fmt.Sprintf("Sending results to overseer (status: %s)", status))

	// Send full execution result to overseer via MCP
	notifyTaskCompleteWithResult(mcpClient, bead, result, status, duration, exitCode, agentID)

	// Optional: Also try direct retention as fallback (best-effort, non-blocking)
	// This provides redundancy in case overseer batching fails
	if config.HindsightClient.Enabled {
		go func() {
			recordExecutionResult(bead, result, mcpClient, config)
		}()
	}

	// STEP 5: Close the bead using beads CLI (best-effort)
	// Only close beads that succeeded - failed beads remain open for retry/investigation
	if exitCode == 0 && result.Success {
		if err := closeBeadFromWorker(bead, projectPath, config.Repo, "completed"); err != nil {
			log.Printf("[WARN] Failed to close bead from worker (non-fatal): %v", err)
			// Don't fail the worker just because we couldn't close the bead
			// The overseer can handle this asynchronously
		} else {
			log.Printf("✅ Successfully closed bead %s with status: completed", beadID)
		}
	} else {
		log.Printf("⚠️  Bead %s NOT closed - worker failed (exit: %d, success: %v). Will remain open for retry.",
			beadID, exitCode, result.Success)
	}

	streamLog(mcpClient, "info", fmt.Sprintf("Worker completed for bead %s with status %s", beadID, status))

	if result != nil && result.Output != "" {
		log.Printf("📄 Execution output:\n%s", result.Output)
	}

	if result != nil && result.Error != "" {
		log.Printf("❌ Execution error:\n%s", result.Error)
	}

	log.Printf("✅ Worker completed for bead: %s (status: %s, duration: %v, attempts: %d)",
		beadID, status, duration, result.Attempts)

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
	scoreStr := os.Getenv("HYDRA_HISTORIESIGHT_MIN_SCORE")
	if scoreStr != "" {
		if f, err := strconv.ParseFloat(scoreStr, 64); err == nil {
			cfg.MinScore = f
		} else {
			cfg.MinScore = 0.7
		}
	} else {
		cfg.MinScore = 0.7
	}

	return cfg
}

// parsePiConfig parses Pi agent configuration from environment variables
func parsePiConfig() PiConfig {
	var cfg PiConfig

	// Check if Pi is enabled
	enabledStr := os.Getenv("HYDRA_PI_ENABLED")
	cfg.Enabled = enabledStr == "true" || enabledStr == "1"

	// Parse provider
	cfg.Provider = os.Getenv("HYDRA_PI_PROVIDER")
	if cfg.Provider == "" {
		cfg.Provider = "kimi-coding" // Default provider
	}

	// Parse model
	cfg.Model = os.Getenv("HYDRA_PI_MODEL")
	if cfg.Model == "" {
		cfg.Model = "k2.5" // Default model
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

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := calculateBackoff(attempt, policy)
			log.Printf("⏳ Retry %d/%d: waiting %v before next attempt...", attempt, policy.MaxRetries, backoff)
			streamLog(mcpClient, "info", fmt.Sprintf("Retry %d/%d: waiting %v", attempt, policy.MaxRetries, backoff))
			time.Sleep(backoff)
		}

		log.Printf("🔄 Execution attempt %d/%d", attempt+1, policy.MaxRetries+1)
		streamLog(mcpClient, "info", fmt.Sprintf("Execution attempt %d/%d", attempt+1, policy.MaxRetries+1))

		// Query Hindsight for context enrichment before execution (only on first attempt)
		var hindsightContext *agents.ContextEnrichment
		if attempt == 0 {
			hindsightContext = queryHindsightForContext(bead, config, mcpClient)
		}

		// Choose execution method based on configuration
		if config.Pi.Enabled {
			result, lastErr = executeWithPi(bead, config.Pi, mcpClient, projectPath, hindsightContext)
		} else {
			result, lastErr = executeWithOpencode(bead, agentType, mcpClient, projectPath, hindsightContext)
		}
		result.Attempts = attempt + 1

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

// shouldRetry determines if an error should be retried
func shouldRetry(err error, attempt int, policy RetryPolicy) bool {
	// Don't retry if we've exhausted all retries
	if attempt >= policy.MaxRetries {
		return false
	}

	if err == nil {
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

// executeWithOpencode executes the opencode CLI with activity-based timeout monitoring
// The timeout resets whenever there's output activity (logs, messages, questions)
func executeWithOpencode(
	bead types.Bead,
	agentType string,
	mcpClient *mcp.Client,
	projectPath string,
	hindsightContext *agents.ContextEnrichment,
) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Success:   false,
		AgentType: agentType,
	}

	// Activity-based timeout: 5 minutes of inactivity
	activityTimeout := 5 * time.Minute
	activityMonitor := NewActivityMonitor(activityTimeout)

	// Build opencode command
	cmd := buildOpencodeCommand(bead, agentType, projectPath, hindsightContext)

	log.Printf("🔧 Executing: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))
	log.Printf("📁 Working directory: %s", cmd.Dir)
	log.Printf("⏱️  Activity timeout: %v (resets on any output)", activityTimeout)
	streamLog(mcpClient, "info", fmt.Sprintf("Executing opencode command in %s with activity monitoring", cmd.Dir))

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

	// Start the command
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("failed to start opencode: %w", err)
	}

	// Capture output with activity monitoring
	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup

	// Monitor stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stdoutBuf.WriteString(line + "\n")
			activityMonitor.RecordActivity()
			// Also stream to MCP for real-time monitoring
			if mcpClient != nil {
				streamLog(mcpClient, "info", fmt.Sprintf("[stdout] %s", line))
			}
		}
	}()

	// Monitor stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stderrBuf.WriteString(line + "\n")
			activityMonitor.RecordActivity()
			// Also stream to MCP for real-time monitoring
			if mcpClient != nil {
				streamLog(mcpClient, "warn", fmt.Sprintf("[stderr] %s", line))
			}
		}
	}()

	// Wait for completion or timeout
	done := make(chan error, 1)
	go func() {
		wg.Wait() // Wait for stdout/stderr readers to finish
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
			if activityMonitor.IsTimedOut() {
				log.Printf("⏰ Activity timeout! No output for %v. Killing process group...", activityMonitor.TimeSinceActivity())
				// Kill entire process group using negative PID
				if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
					log.Printf("Failed to kill process group: %v", err)
					// Fallback: try killing just the main process
					if err := cmd.Process.Kill(); err != nil {
						log.Printf("Failed to kill main process: %v", err)
					}
				}
				// Additional cleanup: kill any remaining children
				cleanupCmd := exec.Command("pkill", "-P", fmt.Sprintf("%d", cmd.Process.Pid))
				cleanupCmd.Run() // Ignore errors - best effort cleanup
				result.ExitCode = -1
				result.Success = false
				result.Error = fmt.Sprintf("Activity timeout: no output for %v", activityTimeout)
				return result, fmt.Errorf("activity timeout after %v of inactivity", activityTimeout)
			}
			// Log activity status periodically
			if timeSince := activityMonitor.TimeSinceActivity(); timeSince > 1*time.Minute {
				log.Printf("⏳ Waiting... Last activity %v ago", timeSince)
			}
		}
	}

	result.Output = stdoutBuf.String()
	result.Error = stderrBuf.String()

	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
		result.Success = false

		log.Printf("❌ Opencode failed with exit code %d", result.ExitCode)
		if stdoutStr := stdoutBuf.String(); stdoutStr != "" {
			log.Printf("📤 STDOUT:\n%s", stdoutStr)
		}
		if stderrStr := stderrBuf.String(); stderrStr != "" {
			log.Printf("📤 STDERR:\n%s", stderrStr)
		}

		return result, fmt.Errorf("opencode execution failed: %w", cmdErr)
	}

	result.ExitCode = 0
	result.Success = true
	log.Printf("✅ Opencode completed successfully (last activity %v ago)", activityMonitor.TimeSinceActivity())
	return result, nil
}

// buildOpencodeCommand builds the opencode CLI command for a bead
// Opencode run syntax: echo "prompt" | opencode run --format json --dir <path>
func buildOpencodeCommand(bead types.Bead, agentType string, projectPath string, hindsightContext *agents.ContextEnrichment) *exec.Cmd {
	// Find opencode binary
	opencodePath := os.Getenv("HYDRA_OPCODE_PATH")
	if opencodePath == "" {
		opencodePath = "opencode"
	}

	// Use project path if provided, otherwise fall back to project name
	path := projectPath
	if path == "" {
		path = bead.Project
	}

	// Build the prompt message from bead info
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

	// Add target files if available
	targetFiles := extractTargetFiles(bead)
	if len(targetFiles) > 0 {
		promptParts = append(promptParts, fmt.Sprintf("Files to modify: %v", targetFiles))
	}

	// Add Hindsight context if available
	if hindsightContext != nil && len(hindsightContext.Memories) > 0 {
		promptParts = append(promptParts, "\n--- Relevant Context from Previous Work ---")
		for i, mem := range hindsightContext.Memories {
			promptParts = append(promptParts, fmt.Sprintf("%d. %s (relevance: %.2f)\n   %s",
				i+1, mem.ID, mem.Score, mem.Content))
		}
		promptParts = append(promptParts, "--- End Context ---\n")
	}

	// Join all parts into single prompt
	prompt := strings.Join(promptParts, "\n")

	// Build command: echo "prompt" | opencode run --format json --dir <path> --title <name>
	// We use bash to pipe the prompt to opencode
	// Use bead ID as the title so we can identify which worker is working on which bead
	title := fmt.Sprintf("Hydra:%s", bead.ID)
	cmd := exec.Command("bash", "-c", fmt.Sprintf(
		"echo %q | %s run --format json --dir %q --title %q",
		prompt,
		opencodePath,
		path,
		title,
	))

	// Create new process group so we can kill the entire tree (bash + opencode + children)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Add environment variables
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HYDRA_AGENT_TYPE=%s", agentType),
		fmt.Sprintf("HYDRA_BEAD_TYPE=%s", bead.Type),
	)

	return cmd
}

// executeWithPi executes the Pi agent CLI with activity-based timeout monitoring
func executeWithPi(
	bead types.Bead,
	piConfig PiConfig,
	mcpClient *mcp.Client,
	projectPath string,
	hindsightContext *agents.ContextEnrichment,
) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Success:   false,
		AgentType: "pi",
	}

	// Activity-based timeout: 5 minutes of inactivity
	activityTimeout := 5 * time.Minute
	activityMonitor := NewActivityMonitor(activityTimeout)

	// Build pi command
	cmd := buildPiCommand(bead, piConfig, projectPath, hindsightContext)

	log.Printf("🔧 Executing Pi: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))
	log.Printf("📁 Working directory: %s", cmd.Dir)
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

	// Create stdin pipe for JSONL protocol
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return result, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	defer stdinPipe.Close()

	// Start the command
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("failed to start pi: %w", err)
	}

	// Build the prompt message
	prompt := buildPiPrompt(bead, hindsightContext)

	// Send prompt as JSONL
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "pi/execute",
		"params": map[string]interface{}{
			"prompt": prompt,
		},
		"id": 1,
	}

	encoder := json.NewEncoder(stdinPipe)
	if err := encoder.Encode(request); err != nil {
		return result, fmt.Errorf("failed to send prompt to pi: %w", err)
	}
	stdinPipe.Close()

	// Capture output with activity monitoring
	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup

	// Monitor stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stdoutBuf.WriteString(line + "\n")
			activityMonitor.RecordActivity()
			// Also stream to MCP for real-time monitoring
			if mcpClient != nil {
				streamLog(mcpClient, "info", fmt.Sprintf("[pi-stdout] %s", line))
			}
		}
	}()

	// Monitor stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stderrBuf.WriteString(line + "\n")
			activityMonitor.RecordActivity()
			// Also stream to MCP for real-time monitoring
			if mcpClient != nil {
				streamLog(mcpClient, "warn", fmt.Sprintf("[pi-stderr] %s", line))
			}
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
			if activityMonitor.IsTimedOut() {
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

	// Determine success based on exit code
	if cmdErr == nil && result.ExitCode == 0 {
		result.Success = true
		log.Printf("✅ Pi completed successfully (last activity %v ago)", activityMonitor.TimeSinceActivity())
	} else {
		result.Success = false
		log.Printf("❌ Pi failed with exit code %d", result.ExitCode)
		if stdoutStr := stdoutBuf.String(); stdoutStr != "" {
			log.Printf("📤 STDOUT:\n%s", stdoutStr)
		}
		if stderrStr := stderrBuf.String(); stderrStr != "" {
			log.Printf("📤 STDERR:\n%s", stderrStr)
		}
	}

	return result, cmdErr
}

// buildPiCommand builds the Pi agent CLI command
func buildPiCommand(bead types.Bead, piConfig PiConfig, projectPath string, hindsightContext *agents.ContextEnrichment) *exec.Cmd {
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

	// Build base args
	args := []string{
		"--mode", "rpc",
		"--no-session",
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
		skillResult, err := pi.BuildSkillFlags(piConfig.ConfigDir)
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

	// Set PI_CODING_AGENT_DIR if configDir is set
	if piConfig.ConfigDir != "" {
		agentDir := filepath.Join(piConfig.ConfigDir, "agent")
		absAgentDir, err := filepath.Abs(agentDir)
		if err == nil {
			envVars = append(envVars, fmt.Sprintf("PI_CODING_AGENT_DIR=%s", absAgentDir))
		}
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

	return cmd
}

// buildPiPrompt builds the prompt message for Pi agent
func buildPiPrompt(bead types.Bead, hindsightContext *agents.ContextEnrichment) string {
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

	// Add target files if available
	targetFiles := extractTargetFiles(bead)
	if len(targetFiles) > 0 {
		promptParts = append(promptParts, fmt.Sprintf("Files to modify: %v", targetFiles))
	}

	// Add Hindsight context if available
	if hindsightContext != nil && len(hindsightContext.Memories) > 0 {
		promptParts = append(promptParts, "\nRelevant past executions:")
		for _, mem := range hindsightContext.Memories {
			promptParts = append(promptParts, fmt.Sprintf("- %s (score: %.2f): %s",
				mem.ID, mem.Score, truncateString(mem.Content, 200)))
		}
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
		hindsight.WithMinScore(config.HindsightClient.MinScore),
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

		// Log each memory
		for i, mem := range enrichment.Memories {
			log.Printf("   Memory %d: %s (score: %.2f) - %s",
				i+1, mem.ID, mem.Score, truncateString(mem.Content, 100))
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
			hindsight.WithMinScore(config.HindsightClient.MinScore),
			hindsight.WithEnabled(true),
		)
		log.Printf("[Hindsight] Client initialized (baseURL: %s, limit: %d, minScore: %.2f)",
			config.HindsightClient.BaseURL, config.HindsightClient.Limit, config.HindsightClient.MinScore)
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
			result.Output,
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
