package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// WorkerConfig holds the worker configuration from environment variables
type WorkerConfig struct {
	BeadID    string
	Project   string
	Repo      string
	AgentType string
	MCP       struct {
		Enabled bool
		URL     string
	}
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

	log.Printf("🚀 Worker starting for bead: %s", beadID)
	log.Printf("   Project: %s, Repo: %s, Agent: %s", config.Project, config.Repo, config.AgentType)
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
	exitCode := 0
	success := true

	// TODO:
	// 1. Query cass_memory for patterns
	// 2. Use llm-tldr to understand codebase
	// 3. Determine agent type from bead
	// 4. Execute with opencode
	// 5. Record results to cass_memory
	// 6. Send completion message via MCP

	// Simulate work for now
	time.Sleep(2 * time.Second)

	// Calculate duration
	duration := time.Since(startTime)

	// Determine status
	status := "success"
	if !success {
		status = "failure"
		exitCode = 1
	}

	// Notify task completion
	notifyTaskComplete(mcpClient, bead, status, duration, exitCode)

	streamLog(mcpClient, "info", fmt.Sprintf("Worker completed for bead %s with status %s", beadID, status))

	log.Printf("✅ Worker completed for bead: %s (status: %s, duration: %v)", beadID, status, duration)

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
