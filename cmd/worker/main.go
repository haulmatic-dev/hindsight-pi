package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/internal/mcp"
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
	MCP             struct {
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

	result, err := executeWithRetry(bead, determinedAgentType, mcpClient, retryPolicy, projectPath)

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

	recordExecutionResult(bead, result, mcpClient)

	// Notify task completion
	notifyTaskComplete(mcpClient, bead, status, duration, exitCode)

	// STEP 5: Close the bead using beads CLI (best-effort)
	// This makes the worker self-contained and resilient to overseer failures
	closeReason := "completed"
	if exitCode != 0 {
		closeReason = "failed"
	}

	if err := closeBeadFromWorker(bead, projectPath, config.Repo, closeReason); err != nil {
		log.Printf("[WARN] Failed to close bead from worker (non-fatal): %v", err)
		// Don't fail the worker just because we couldn't close the bead
		// The overseer can handle this asynchronously
	} else {
		log.Printf("✅ Successfully closed bead %s with status: %s", beadID, closeReason)
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
func parseConfig() WorkerConfig {
	var config WorkerConfig

	config.BeadID = os.Getenv("HYDRA_BEAD_ID")
	config.Project = os.Getenv("HYDRA_PROJECT")
	config.ProjectPath = os.Getenv("HYDRA_PROJECT_PATH")
	config.Repo = os.Getenv("HYDRA_REPO")
	config.AgentType = os.Getenv("HYDRA_AGENT_TYPE")
	config.BeadTitle = os.Getenv("HYDRA_BEAD_TITLE")
	config.BeadDescription = os.Getenv("HYDRA_BEAD_DESCRIPTION")
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

// executeWithRetry executes the bead with exponential backoff retry logic
func executeWithRetry(
	bead types.Bead,
	agentType string,
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

		result, lastErr = executeWithOpencode(bead, agentType, mcpClient, projectPath)
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
) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Success:   false,
		AgentType: agentType,
	}

	// Activity-based timeout: 5 minutes of inactivity
	activityTimeout := 5 * time.Minute
	activityMonitor := NewActivityMonitor(activityTimeout)

	// Build opencode command
	cmd := buildOpencodeCommand(bead, agentType, projectPath)

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
func buildOpencodeCommand(bead types.Bead, agentType string, projectPath string) *exec.Cmd {
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

	// Note: Intelligence context now accessed via skills on-demand, not force-fed
	// Pi agents use cass-memory-insights and tldr-code-intelligence skills

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

// recordExecutionResult records the execution outcome to cass_memory
func recordExecutionResult(
	bead types.Bead,
	result *ExecutionResult,
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
