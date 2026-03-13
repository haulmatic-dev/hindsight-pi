package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// OpencodeConnector implements AgentConnector for opencode
// Uses HTTP server mode for better control and session management
type OpencodeConnector struct {
	serverURL string
	serverCmd *exec.Cmd
	port      int
	hostname  string
}

// NewOpencodeConnector creates a new opencode connector
func NewOpencodeConnector(port int) *OpencodeConnector {
	if port == 0 {
		port = 4096 // default opencode port
	}
	return &OpencodeConnector{
		serverURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		port:      port,
		hostname:  "127.0.0.1",
	}
}

// Start initializes the opencode server and creates a session
func (c *OpencodeConnector) Start(ctx context.Context, bead types.Bead, opts AgentOptions) (*AgentSession, error) {
	// Ensure server is running
	if err := c.ensureServer(ctx); err != nil {
		return nil, fmt.Errorf("failed to start opencode server: %w", err)
	}

	// Create a new session via HTTP API
	sessionID := fmt.Sprintf("hydra-%s", bead.ID)
	createReq := map[string]interface{}{
		"title": fmt.Sprintf("Hydra: %s", bead.Title),
	}

	if err := c.post(ctx, "/session", createReq, nil); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	session := &AgentSession{
		ID:        sessionID,
		BeadID:    bead.ID,
		StartTime: time.Now(),
		connector: c,
		metadata: map[string]interface{}{
			"session_id":   sessionID,
			"project_path": opts.ProjectPath,
		},
	}

	return session, nil
}

// Execute sends the prompt to opencode and waits for completion
func (c *OpencodeConnector) Execute(ctx context.Context, session *AgentSession, prompt string) (*AgentResult, error) {
	startTime := time.Now()

	// Build the prompt with intelligence context
	fullPrompt := c.buildPrompt(prompt, session)

	// Send prompt via HTTP API
	promptReq := map[string]interface{}{
		"parts": []map[string]string{
			{"type": "text", "text": fullPrompt},
		},
	}

	var response map[string]interface{}
	log.Printf("[OpencodeConnector] Sending prompt to session %s...", session.ID)
	if err := c.post(ctx, fmt.Sprintf("/session/%s/message", session.ID), promptReq, &response); err != nil {
		log.Printf("[OpencodeConnector] Execute failed: %v", err)
		return &AgentResult{
			Success:  false,
			ExitCode: 1,
			Error:    err.Error(),
			Duration: time.Since(startTime),
		}, nil
	}

	duration := time.Since(startTime)

	// Handle empty response (message endpoint returns empty body on success)
	if response == nil {
		log.Printf("[OpencodeConnector] Execute completed with empty response (async mode)")
		return &AgentResult{
			Success:  true,
			ExitCode: 0,
			Output:   "Task submitted successfully",
			Duration: duration,
		}, nil
	}

	log.Printf("[OpencodeConnector] Execute response: %+v", response)

	// Extract result from response
	output := ""
	if info, ok := response["info"].(map[string]interface{}); ok {
		if parts, ok := info["parts"].([]interface{}); ok && len(parts) > 0 {
			if firstPart, ok := parts[0].(map[string]interface{}); ok {
				if text, ok := firstPart["text"].(string); ok {
					output = text
				}
			}
		}
	}

	return &AgentResult{
		Success:  true,
		ExitCode: 0,
		Output:   output,
		Duration: duration,
	}, nil
}

// Abort forcibly stops the session
func (c *OpencodeConnector) Abort(ctx context.Context, session *AgentSession) error {
	// Use the abort endpoint
	return c.post(ctx, fmt.Sprintf("/session/%s/abort", session.ID), nil, nil)
}

// Status returns current session status
func (c *OpencodeConnector) Status(ctx context.Context, session *AgentSession) (AgentStatus, error) {
	var response map[string]interface{}
	if err := c.get(ctx, fmt.Sprintf("/session/%s", session.ID), &response); err != nil {
		return AgentStatus{State: AgentStateUnknown}, err
	}

	status := AgentStatus{
		LastActivity: time.Now(),
	}

	// Parse state from response
	if state, ok := response["status"].(string); ok {
		switch state {
		case "running":
			status.State = AgentStateRunning
		case "completed":
			status.State = AgentStateCompleted
		case "failed":
			status.State = AgentStateFailed
		default:
			status.State = AgentStateUnknown
		}
	}

	return status, nil
}

// Cleanup closes the session
func (c *OpencodeConnector) Cleanup(ctx context.Context, session *AgentSession) error {
	// Delete the session
	return c.delete(ctx, fmt.Sprintf("/session/%s", session.ID), nil)
}

// IsHealthy checks if server is responsive
func (c *OpencodeConnector) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var health map[string]interface{}
	if err := c.get(ctx, "/global/health", &health); err != nil {
		return false
	}

	healthy, _ := health["healthy"].(bool)
	return healthy
}

// ensureServer starts opencode server if not running
func (c *OpencodeConnector) ensureServer(ctx context.Context) error {
	if c.IsHealthy() {
		return nil
	}

	log.Printf("[OpencodeConnector] Starting opencode server on port %d...", c.port)

	// Start opencode server from a neutral directory (not a project directory)
	// This prevents opencode from trying to load project-specific configs
	c.serverCmd = exec.CommandContext(ctx, "opencode", "serve",
		"--port", fmt.Sprintf("%d", c.port),
		"--hostname", c.hostname,
	)

	// Run from home directory to avoid project-specific issues
	c.serverCmd.Dir = os.Getenv("HOME")

	c.serverCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Capture output for debugging
	c.serverCmd.Stdout = nil
	c.serverCmd.Stderr = nil

	if err := c.serverCmd.Start(); err != nil {
		return fmt.Errorf("failed to start opencode server: %w", err)
	}

	log.Printf("[OpencodeConnector] Opencode server started with PID %d, waiting for health check...", c.serverCmd.Process.Pid)

	// Wait for server to be ready (with retry)
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		time.Sleep(500 * time.Millisecond)
		if c.IsHealthy() {
			log.Printf("[OpencodeConnector] Opencode server is healthy on port %d", c.port)
			return nil
		}
	}

	// If we get here, server didn't become healthy
	if c.serverCmd.Process != nil {
		c.serverCmd.Process.Kill()
	}
	return fmt.Errorf("opencode server failed to become healthy after %d retries", maxRetries)
}

// buildPrompt constructs the full prompt with intelligence context
func (c *OpencodeConnector) buildPrompt(basePrompt string, session *AgentSession) string {
	var buf bytes.Buffer

	// Add patterns if available
	if patterns, ok := session.metadata["patterns"].([]Pattern); ok && len(patterns) > 0 {
		buf.WriteString("## Relevant Patterns from Previous Work:\n")
		for i, p := range patterns {
			if i >= 3 { // Limit to 3 patterns
				break
			}
			buf.WriteString(fmt.Sprintf("%d. %s (confidence: %.2f)\n   %s\n\n",
				i+1, p.Title, p.Confidence, p.Content))
		}
	}

	// Add anti-patterns
	if antiPatterns, ok := session.metadata["anti_patterns"].([]AntiPattern); ok && len(antiPatterns) > 0 {
		buf.WriteString("## Anti-Patterns to Avoid:\n")
		for i, ap := range antiPatterns {
			if i >= 3 {
				break
			}
			buf.WriteString(fmt.Sprintf("%d. %s\n   %s\n\n",
				i+1, ap.Title, ap.Content))
		}
	}

	buf.WriteString("## Task:\n")
	buf.WriteString(basePrompt)

	return buf.String()
}

// HTTP helper methods
func (c *OpencodeConnector) get(ctx context.Context, path string, result interface{}) error {
	url := c.serverURL + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (c *OpencodeConnector) post(ctx context.Context, path string, body, result interface{}) error {
	url := c.serverURL + path

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	log.Printf("[OpencodeConnector] HTTP POST %s", url)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[OpencodeConnector] HTTP POST failed: %v", err)
		return err
	}
	defer resp.Body.Close()
	log.Printf("[OpencodeConnector] HTTP POST response: %d %s", resp.StatusCode, resp.Status)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read raw body
	bodyBytes, _ := io.ReadAll(resp.Body)

	// Handle empty response (success case for message endpoint)
	if len(bodyBytes) == 0 {
		log.Printf("[OpencodeConnector] HTTP response body empty (success)")
		return nil
	}

	log.Printf("[OpencodeConnector] HTTP response body (len=%d): %s", len(bodyBytes), string(bodyBytes))

	if result != nil {
		return json.Unmarshal(bodyBytes, result)
	}
	return nil
}

func (c *OpencodeConnector) delete(ctx context.Context, path string, result interface{}) error {
	url := c.serverURL + path
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}
