package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// WorkerConfig holds the worker configuration from environment variables
type WorkerConfig struct {
	BeadID      string
	Project     string
	ProjectPath string
	Repo        string
	AgentType   string
	MCP         struct {
		Enabled bool
		URL     string
	}
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

// IntelligenceContext holds pre-execution intelligence
type IntelligenceContext struct {
	Patterns     []intelligence.Bullet
	AntiPatterns []intelligence.Bullet
	Impact       *intelligence.TLDRImpactResponse
	RelatedFiles []intelligence.TLDRSemanticResult
	CassContext  *intelligence.CassContextResponse
}

var (
	agentID   string
	agentType types.AgentType
)

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
		ID:      beadID,
		Project: config.Project,
		Repo:    config.Repo,
		Type:    config.AgentType,
	}

	// Use project path if available, otherwise fall back to project name
	projectPath := config.ProjectPath
	if projectPath == "" {
		projectPath = config.Project
	}

	log.Printf("🚀 Worker starting for bead: %s", beadID)
	log.Printf("   Project: %s (path: %s), Repo: %s, Agent: %s", config.Project, projectPath, config.Repo, config.AgentType)
	log.Printf("   Agent ID: %s", agentID)

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

	// STEP 2: Gather pre-execution intelligence
	log.Printf("🧠 Gathering pre-execution intelligence...")
	streamLog(mcpClient, "info", "Gathering pre-execution intelligence...")

	intelligenceCtx, err := gatherPreExecutionIntelligence(bead)
	if err != nil {
		log.Printf("[WARN] Failed to gather intelligence: %v", err)
		streamLog(mcpClient, "warn", fmt.Sprintf("Intelligence gathering failed: %v", err))
		// Continue with empty intelligence context for graceful degradation
		intelligenceCtx = &IntelligenceContext{}
	} else {
		log.Printf("✅ Intelligence gathered: %d patterns, %d anti-patterns, %d related files",
			len(intelligenceCtx.Patterns), len(intelligenceCtx.AntiPatterns), len(intelligenceCtx.RelatedFiles))
		streamLog(mcpClient, "info", fmt.Sprintf("Intelligence: %d patterns found", len(intelligenceCtx.Patterns)))
	}

	// STEP 3: Execute with retry policy
	retryPolicy := DefaultRetryPolicy()
	log.Printf("⚙️  Executing with retry policy (max %d retries)...", retryPolicy.MaxRetries)
	streamLog(mcpClient, "info", fmt.Sprintf("Starting execution (max %d retries)", retryPolicy.MaxRetries))

	result, err := executeWithRetry(bead, determinedAgentType, intelligenceCtx, mcpClient, retryPolicy, projectPath)

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

	// STEP 4: Record execution results
	log.Printf("📝 Recording execution results...")
	streamLog(mcpClient, "info", fmt.Sprintf("Recording results (status: %s)", status))

	recordExecutionResult(bead, result, intelligenceCtx, mcpClient)

	// Notify task completion
	notifyTaskComplete(mcpClient, bead, status, duration, exitCode)

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
func parseConfig() WorkerConfig {
	var config WorkerConfig

	config.BeadID = os.Getenv("HYDRA_BEAD_ID")
	config.Project = os.Getenv("HYDRA_PROJECT")
	config.ProjectPath = os.Getenv("HYDRA_PROJECT_PATH")
	config.Repo = os.Getenv("HYDRA_REPO")
	config.AgentType = os.Getenv("HYDRA_AGENT_TYPE")
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

// gatherPreExecutionIntelligence gathers intelligence from cass_memory and TLDR
func gatherPreExecutionIntelligence(bead types.Bead) (*IntelligenceContext, error) {
	ctx := &IntelligenceContext{
		Patterns:     []intelligence.Bullet{},
		AntiPatterns: []intelligence.Bullet{},
		RelatedFiles: []intelligence.TLDRSemanticResult{},
	}

	// Build task query from bead description and title
	taskQuery := bead.Title
	if bead.Description != "" {
		taskQuery = fmt.Sprintf("%s %s", bead.Title, bead.Description)
	}

	// Query cass_memory for patterns and anti-patterns
	cassClient := intelligence.NewCassMemoryClient()
	if cassClient.IsEnabled() {
		cassCtx, err := cassClient.Context(taskQuery)
		if err != nil {
			log.Printf("[WARN] Failed to query cass_memory: %v", err)
		} else {
			ctx.CassContext = cassCtx
			ctx.Patterns = cassCtx.Data.RelevantBullets
			ctx.AntiPatterns = cassCtx.Data.AntiPatterns
		}
	} else {
		log.Println("ℹ️ cass_memory client not enabled, skipping pattern lookup")
	}

	// Query TLDR for impact analysis and related files
	tldrClient := intelligence.NewTLDRClient("")
	if tldrClient.IsAvailable() {
		// Get target files for impact analysis
		targetFiles := extractTargetFiles(bead)

		if len(targetFiles) > 0 {
			// Analyze first target file for impact
			impact, err := tldrClient.ImpactAnalysis(targetFiles[0], 2)
			if err != nil {
				log.Printf("[WARN] Failed to get impact analysis: %v", err)
			} else {
				ctx.Impact = impact
			}
		}

		// Perform semantic search for related files
		semanticResults, err := tldrClient.SemanticSearch(taskQuery)
		if err != nil {
			log.Printf("[WARN] Failed semantic search: %v", err)
		} else {
			ctx.RelatedFiles = semanticResults.Results
		}
	} else {
		log.Println("ℹ️ TLDR daemon not available, skipping impact analysis")
	}

	return ctx, nil
}

// executeWithRetry executes the bead with exponential backoff retry logic
func executeWithRetry(
	bead types.Bead,
	agentType string,
	intelligenceCtx *IntelligenceContext,
	mcpClient *mcp.Client,
	policy RetryPolicy,
	projectPath string,
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

		result, lastErr = executeWithOpencode(bead, agentType, intelligenceCtx, mcpClient, projectPath)
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

// executeWithOpencode executes the opencode CLI with the given bead and intelligence
func executeWithOpencode(
	bead types.Bead,
	agentType string,
	intelligenceCtx *IntelligenceContext,
	mcpClient *mcp.Client,
	projectPath string,
) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Success:   false,
		AgentType: agentType,
	}

	// Build opencode command
	cmd := buildOpencodeCommand(bead, agentType, intelligenceCtx, projectPath)

	log.Printf("🔧 Executing: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))
	log.Printf("📁 Working directory: %s", cmd.Dir)
	streamLog(mcpClient, "info", fmt.Sprintf("Executing opencode command in %s", cmd.Dir))

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err := cmd.Run()

	result.Output = stdout.String()
	result.Error = stderr.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
		result.Success = false

		// Log the actual error output for debugging
		log.Printf("❌ Opencode failed with exit code %d", result.ExitCode)
		if stdoutStr := stdout.String(); stdoutStr != "" {
			log.Printf("📤 STDOUT:\n%s", stdoutStr)
		}
		if stderrStr := stderr.String(); stderrStr != "" {
			log.Printf("📤 STDERR:\n%s", stderrStr)
		}

		return result, fmt.Errorf("opencode execution failed: %w", err)
	}

	result.ExitCode = 0
	result.Success = true
	return result, nil
}

// buildOpencodeCommand builds the opencode CLI command for a bead
// Opencode expects: opencode run <message> OR opencode <project-path>
func buildOpencodeCommand(bead types.Bead, agentType string, intelligenceCtx *IntelligenceContext, projectPath string) *exec.Cmd {
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

	// Start with agent type context
	promptParts = append(promptParts, fmt.Sprintf("[%s]", agentType))

	// Add bead type and title
	if bead.Title != "" {
		promptParts = append(promptParts, fmt.Sprintf("%s: %s", bead.Type, bead.Title))
	} else {
		promptParts = append(promptParts, bead.Type)
	}

	// Add description
	if bead.Description != "" {
		promptParts = append(promptParts, "- "+bead.Description)
	}

	// Add target files if available
	targetFiles := extractTargetFiles(bead)
	if len(targetFiles) > 0 {
		promptParts = append(promptParts, fmt.Sprintf("Files: %v", targetFiles))
	}

	// Join all parts into single prompt
	prompt := strings.Join(promptParts, " ")

	// Create command with proper working directory
	cmd := exec.Command(opencodePath, "run", prompt)
	cmd.Dir = path

	// Add environment variable for agent type
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HYDRA_AGENT_TYPE=%s", agentType),
		fmt.Sprintf("HYDRA_BEAD_TYPE=%s", bead.Type),
	)

	return cmd
}

// recordExecutionResult records the execution outcome to cass_memory
func recordExecutionResult(
	bead types.Bead,
	result *ExecutionResult,
	intelligenceCtx *IntelligenceContext,
	mcpClient *mcp.Client,
) {
	if result == nil {
		log.Println("[WARN] Cannot record nil result")
		return
	}

	// Initialize cass_memory client
	cassClient := intelligence.NewCassMemoryClient()
	if !cassClient.IsEnabled() {
		log.Println("ℹ️ cass_memory not enabled, skipping result recording")
		return
	}

	// Determine status for recording
	status := "failure"
	if result.Success {
		status = "success"
	}

	// Build success/failure pattern
	pattern := buildSuccessPattern(bead, result)
	if pattern == "" {
		log.Println("[WARN] Empty pattern generated, skipping recording")
		return
	}

	// Get category for agent type
	category := intelligence.GetCategoryForAgentType(result.AgentType)

	// Add pattern to cass_memory
	if err := cassClient.AddPattern(pattern, category); err != nil {
		log.Printf("[WARN] Failed to record pattern: %v", err)
		streamLog(mcpClient, "warn", fmt.Sprintf("Failed to record pattern: %v", err))
	} else {
		log.Printf("✅ Recorded %s pattern to cass_memory (category: %s)", status, category)
		streamLog(mcpClient, "info", fmt.Sprintf("Recorded %s pattern to cass_memory", status))
	}

	// Record outcome for any matched patterns
	if intelligenceCtx != nil && intelligenceCtx.CassContext != nil {
		var ruleIDs []string
		for _, bullet := range intelligenceCtx.CassContext.Data.RelevantBullets {
			if bullet.ID != "" {
				ruleIDs = append(ruleIDs, bullet.ID)
			}
		}

		if len(ruleIDs) > 0 {
			if err := cassClient.RecordOutcome(status, ruleIDs, result.Duration); err != nil {
				log.Printf("[WARN] Failed to record outcome: %v", err)
			} else {
				log.Printf("✅ Recorded outcome for %d rules", len(ruleIDs))
			}
		}
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
