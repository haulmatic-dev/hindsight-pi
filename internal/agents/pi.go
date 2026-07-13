package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/internal/pi"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Executor interface abstracts exec operations for testability
type Executor interface {
	LookPath(file string) (string, error)
	Command(name string, arg ...string) *exec.Cmd
}

// DefaultExecutor implements Executor using the standard exec package
type DefaultExecutor struct{}

func (e *DefaultExecutor) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (e *DefaultExecutor) Command(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}

// Cache TTL for installation status and path lookups
const installationCacheTTL = 5 * time.Minute

// PiConnector implements AgentConnector for Pi coding agent
// Uses RPC mode (JSON protocol over stdin/stdout) for synchronous operation
type PiConnector struct {
	provider      string
	model         string
	thinkingLevel string
	timeout       time.Duration
	apiKeyName    string
	configDir     string
	logger        *log.Logger
	executor      Executor

	// Installation check with TTL-based caching
	mu               sync.RWMutex
	installed        bool
	installCheck     bool
	installCheckTime time.Time

	// Cached paths to reduce PATH lookups
	piPath         string
	npmPath        string
	pathsCached    bool
	pathsCacheTime time.Time
}

// PiConfig contains configuration for Pi connector
type PiConfig struct {
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	ThinkingLevel string `yaml:"thinking_level"`
	Timeout       string `yaml:"timeout"`
	ApiKeyName    string `yaml:"api_key_name"`
	ConfigDir     string `yaml:"config_dir"`
}

// NewPiConnector creates a new Pi connector with the given configuration
func NewPiConnector(config PiConfig, logger *log.Logger) *PiConnector {
	if logger == nil {
		logger = log.Default()
	}

	timeout := 10 * time.Minute
	if config.Timeout != "" {
		if d, err := time.ParseDuration(config.Timeout); err == nil {
			timeout = d
		}
	}

	return &PiConnector{
		provider:      config.Provider,
		model:         config.Model,
		thinkingLevel: config.ThinkingLevel,
		timeout:       timeout,
		apiKeyName:    config.ApiKeyName,
		configDir:     config.ConfigDir,
		logger:        logger,
		executor:      &DefaultExecutor{},
	}
}

// Start initializes a Pi RPC session
// Spawns: pi --mode rpc --provider <provider> --model <model> --no-session
func (c *PiConnector) Start(ctx context.Context, bead types.Bead, opts AgentOptions) (*AgentSession, error) {
	// Check if Pi is installed
	if err := c.ensureInstalled(); err != nil {
		return nil, fmt.Errorf("pi not available: %w", err)
	}

	// Create session ID
	sessionID := fmt.Sprintf("hydra-%s", bead.ID)

	session := &AgentSession{
		ID:        sessionID,
		BeadID:    bead.ID,
		StartTime: time.Now(),
		connector: c,
		metadata: map[string]interface{}{
			"provider":         c.provider,
			"model":            c.model,
			"project":          opts.ProjectPath,
			"bead_title":       bead.Title,
			"bead_description": bead.Description,
		},
	}

	log.Printf("[PiConnector] Started session %s (provider: %s, model: %s)",
		sessionID, c.provider, c.model)

	return session, nil
}

// Execute sends the prompt to Pi and waits for completion
// Uses JSONL protocol over stdin/stdout
func (c *PiConnector) Execute(ctx context.Context, session *AgentSession, prompt string) (*AgentResult, error) {
	startTime := time.Now()

	// Query Hindsight for context enrichment before execution
	enhancedPrompt := c.enrichPromptWithHindsight(session, prompt)

	// Build command. --offline skips Pi's startup provider-list network fetch
	// (flaky under concurrency) in favor of the local models.json cache.
	args := []string{
		"--offline",
		"--mode", "rpc",
		"--no-session",
	}

	if c.provider != "" {
		args = append(args, "--provider", c.provider)
	}

	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	// Get project path from session metadata (used both for skill scoping and
	// as the command's working directory).
	projectPath := ""
	if path, ok := session.metadata["project"].(string); ok {
		projectPath = path
	}

	// Add skill flags if configDir is set, scoped to the project's stack.
	if c.configDir != "" {
		skillResult, err := pi.BuildSkillFlagsForProject(c.configDir, projectPath)
		if err != nil {
			log.Printf("[PiConnector] Warning: failed to build skill flags: %v", err)
		} else {
			args = append(args, skillResult.Flags...)
			log.Printf("[PiConnector] Added %d skill flags (%d found, %d missing)",
				len(skillResult.Flags)/2, len(skillResult.Found), len(skillResult.Missing))

			// Log warnings for missing skills but continue execution
			if skillResult.HasWarnings() {
				skillResult.WarnMissingSkills()
				log.Printf("[PiConnector] ⚠️ Continuing with available skills (missing: %d)", len(skillResult.Missing))
			}
		}
	}

	// Create command with context timeout
	execCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "pi", args...)
	if projectPath != "" {
		cmd.Dir = projectPath
	}

	// Create new process group so we can kill the entire tree (pi + children)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Prepare environment variables
	envVars := os.Environ()
	// Skip Pi's startup network operations (provider-list fetch); use the
	// local models.json cache. Avoids spurious "Unknown provider" flakes when
	// Pi processes launch concurrently.
	envVars = append(envVars, "PI_OFFLINE=1")

	// Set PI_CODING_AGENT_DIR if configDir is set
	if c.configDir != "" {
		agentDir := filepath.Join(c.configDir, "agent")
		absAgentDir, err := filepath.Abs(agentDir)
		if err == nil {
			envVars = append(envVars, fmt.Sprintf("PI_CODING_AGENT_DIR=%s", absAgentDir))
			log.Printf("[PiConnector] Set PI_CODING_AGENT_DIR=%s for bead %s", absAgentDir, session.BeadID)
		} else {
			log.Printf("[PiConnector] Warning: failed to resolve absolute path for agent dir: %v", err)
			envVars = append(envVars, fmt.Sprintf("PI_CODING_AGENT_DIR=%s", agentDir))
		}
	}

	// Set HYDRA_BEAD_TITLE and HYDRA_BEAD_DESCRIPTION from session metadata
	if title, ok := session.metadata["bead_title"].(string); ok && title != "" {
		envVars = append(envVars, fmt.Sprintf("HYDRA_BEAD_TITLE=%s", title))
		log.Printf("[PiConnector] Set HYDRA_BEAD_TITLE for bead %s", session.BeadID)
	}

	if description, ok := session.metadata["bead_description"].(string); ok && description != "" {
		envVars = append(envVars, fmt.Sprintf("HYDRA_BEAD_DESCRIPTION=%s", description))
		log.Printf("[PiConnector] Set HYDRA_BEAD_DESCRIPTION for bead %s", session.BeadID)
	}

	// Set API key from environment if configured
	if c.apiKeyName != "" {
		apiKey := os.Getenv(c.apiKeyName)
		if apiKey != "" {
			envVars = append(envVars, fmt.Sprintf("%s=%s", c.apiKeyName, apiKey))
			log.Printf("[PiConnector] Set API key %s for bead %s", c.apiKeyName, session.BeadID)
		} else {
			log.Printf("[PiConnector] Warning: API key %s not found in environment", c.apiKeyName)
		}
	}

	// Pass Hindsight configuration to worker if enabled
	if os.Getenv("HYDRA_HISTORIESIGHT_ENABLED") == "true" {
		envVars = append(envVars, "HYDRA_HISTORIESIGHT_ENABLED=true")
		envVars = append(envVars, fmt.Sprintf("HYDRA_HISTORIESIGHT_BASE_URL=%s", os.Getenv("HYDRA_HISTORIESIGHT_BASE_URL")))
		envVars = append(envVars, fmt.Sprintf("HYDRA_HISTORIESIGHT_TIMEOUT=%s", os.Getenv("HYDRA_HISTORIESIGHT_TIMEOUT")))
		envVars = append(envVars, fmt.Sprintf("HYDRA_HISTORIESIGHT_LIMIT=%s", os.Getenv("HYDRA_HISTORIESIGHT_LIMIT")))
		envVars = append(envVars, fmt.Sprintf("HYDRA_HISTORIESIGHT_MIN_SCORE=%s", os.Getenv("HYDRA_HISTORIESIGHT_MIN_SCORE")))
		log.Printf("[PiConnector] Passed Hindsight config to worker for bead %s", session.BeadID)
	}

	cmd.Env = envVars

	// Store command in session metadata for potential restart
	session.metadata["cmd"] = cmd

	// Notify about PID availability through callback if provided
	if onPIDAvailable, ok := session.metadata["onPIDAvailable"].(func(int)); ok {
		// Will be called after process starts
		session.metadata["pidCallback"] = onPIDAvailable
	}

	// Get stdin/stdout pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return &AgentResult{
			Success:  false,
			ExitCode: 1,
			Error:    fmt.Sprintf("failed to create stdin pipe: %v", err),
			Duration: time.Since(startTime),
		}, nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &AgentResult{
			Success:  false,
			ExitCode: 1,
			Error:    fmt.Sprintf("failed to create stdout pipe: %v", err),
			Duration: time.Since(startTime),
		}, nil
	}

	// Also capture stderr
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return &AgentResult{
			Success:  false,
			ExitCode: 1,
			Error:    fmt.Sprintf("failed to create stderr pipe: %v", err),
			Duration: time.Since(startTime),
		}, nil
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return &AgentResult{
			Success:  false,
			ExitCode: 1,
			Error:    fmt.Sprintf("failed to start pi: %v", err),
			Duration: time.Since(startTime),
		}, nil
	}

	// Update session with PID and PGID
	session.PID = cmd.Process.Pid
	session.PGID = cmd.Process.Pid // When Setpgid is true, PGID = leader PID

	// Notify about PID availability through callback if provided
	if pidCallback, ok := session.metadata["pidCallback"].(func(int)); ok {
		pidCallback(session.PID)
	}

	// Send prompt command
	promptCmd := map[string]interface{}{
		"type":    "prompt",
		"message": enhancedPrompt,
	}

	if c.thinkingLevel != "" {
		promptCmd["reasoning"] = c.thinkingLevel
	}

	if err := c.sendJSON(stdin, promptCmd); err != nil {
		return &AgentResult{
			Success:  false,
			ExitCode: 1,
			Error:    fmt.Sprintf("failed to send prompt: %v", err),
			Duration: time.Since(startTime),
		}, nil
	}

	// Close stdin to signal end of input
	stdin.Close()

	// Create channels for coordination
	doneChan := make(chan *AgentResult)
	exitChan := make(chan error)

	// Start goroutine to read events
	go func() {
		result := c.readEvents(stdout, stderr, startTime)
		doneChan <- result
	}()

	// Start goroutine to wait for process exit
	go func() {
		exitChan <- cmd.Wait()
	}()

	var result *AgentResult
	var exitErr error

	// Wait for either read completion or process exit
	select {
	case result = <-doneChan:
		// Event reading completed, now check process exit
		select {
		case exitErr = <-exitChan:
			// Process exited
		case <-time.After(5 * time.Second):
			// Timeout waiting for process exit
			log.Printf("[PiConnector] Timeout waiting for process exit after read completion")
			// Kill entire process group using negative PID
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
				log.Printf("[PiConnector] Failed to kill process group: %v", err)
				// Fallback: kill just the main process
				cmd.Process.Kill()
			}
			// Additional cleanup: kill any remaining children
			cleanupCmd := exec.Command("pkill", "-P", fmt.Sprintf("%d", cmd.Process.Pid))
			cleanupCmd.Run() // Ignore errors - best effort cleanup
		}
	case exitErr = <-exitChan:
		// Process exited, now wait for read completion with timeout
		select {
		case result = <-doneChan:
			// Successfully completed reading
		case <-time.After(5 * time.Second):
			// Timeout waiting for read completion
			log.Printf("[PiConnector] Timeout waiting for read completion after process exit")
			result = &AgentResult{
				Success:  false,
				ExitCode: 1,
				Error:    "timeout waiting for output after process exit",
				Duration: time.Since(startTime),
			}
		}
	}

	// Handle process exit error
	if exitErr != nil {
		if exitCodeErr, ok := exitErr.(*exec.ExitError); ok {
			result.ExitCode = exitCodeErr.ExitCode()
			result.Success = false
			result.Error = fmt.Sprintf("pi exited with code %d", result.ExitCode)
		} else {
			result.Success = false
			result.Error = fmt.Sprintf("pi error: %v", exitErr)
		}
	}

	return result, nil
}

// sendJSON sends a JSON command to stdin
func (c *PiConnector) sendJSON(w io.WriteCloser, cmd map[string]interface{}) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// readEvents reads JSONL events from stdout/stderr until agent_end or timeout
func (c *PiConnector) readEvents(stdout, stderr io.ReadCloser, startTime time.Time) *AgentResult {
	result := &AgentResult{
		Success:  true,
		ExitCode: 0,
		Output:   "",
		Duration: time.Since(startTime),
	}

	var outputBuilder strings.Builder
	agentEnded := false
	lastActivity := time.Now()
	lastProgressActivity := time.Now()

	// Activity-based timeout configuration:
	// - activityTimeout: timeout if NO OUTPUT at all for this duration
	// - progressTimeout: timeout if NO PROGRESS EVENTS for this duration (stricter)
	// - maxDuration: hard limit regardless of activity
	activityTimeout := 15 * time.Minute // 15 minutes of total silence
	progressTimeout := 30 * time.Minute // 30 minutes without progress updates
	maxDuration := c.timeout
	if maxDuration == 0 {
		maxDuration = 30 * time.Minute // 30 minute hard limit
	}

	log.Printf("[PiConnector] Starting activity-based monitoring (activity: %v, progress: %v, max: %v)",
		activityTimeout, progressTimeout, maxDuration)

	// Create scanner for stdout with custom reader that respects timeouts
	reader := bufio.NewReader(stdout)

	for {
		// Check for timeout
		if time.Since(startTime) > maxDuration {
			log.Printf("[PiConnector] Max duration exceeded (%v), stopping read", maxDuration)
			result.Success = false
			result.ExitCode = 1
			result.Error = fmt.Sprintf("timeout: exceeded maximum duration of %v", maxDuration)
			break
		}

		// Check for activity timeout (any output at all)
		timeSinceActivity := time.Since(lastActivity)
		if timeSinceActivity > activityTimeout {
			log.Printf("[PiConnector] Activity timeout: no output for %v (max: %v)", timeSinceActivity, activityTimeout)
			result.Success = false
			result.ExitCode = 1
			result.Error = fmt.Sprintf("activity timeout: no output for %v", timeSinceActivity)
			break
		}

		// Check for progress timeout (no progress events - Pi might be stuck)
		timeSinceProgress := time.Since(lastProgressActivity)
		if timeSinceProgress > progressTimeout {
			log.Printf("[PiConnector] Progress timeout: no progress events for %v (max: %v)", timeSinceProgress, progressTimeout)
			result.Success = false
			result.ExitCode = 1
			result.Error = fmt.Sprintf("progress timeout: no activity for %v", timeSinceProgress)
			break
		}

		// Try to read a line with timeout
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// EOF means the process closed stdout
				log.Printf("[PiConnector] EOF reached on stdout")
				break
			}
			// Other error
			log.Printf("[PiConnector] Error reading stdout: %v", err)
			break
		}

		// Update last activity
		lastActivity = time.Now()

		// Trim the line
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try to parse as JSON event
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Not JSON, treat as plain output
			outputBuilder.WriteString(line)
			outputBuilder.WriteString("\n")
			continue
		}

		// Check event type
		eventType, _ := event["type"].(string)

		switch eventType {
		case "agent_start":
			log.Printf("[PiConnector] Agent started processing")

		case "agent_end":
			log.Printf("[PiConnector] Agent completed")
			agentEnded = true
			// Check if there were any errors
			if messages, ok := event["messages"].([]interface{}); ok {
				for _, msg := range messages {
					if msgMap, ok := msg.(map[string]interface{}); ok {
						if stopReason, ok := msgMap["stopReason"].(string); ok {
							if stopReason == "error" || stopReason == "aborted" {
								result.Success = false
								result.ExitCode = 1
								if errMsg, ok := msgMap["errorMessage"].(string); ok {
									result.Error = errMsg
								}
							}
						}
					}
				}
			}
			// Exit after agent_end
			goto done

		case "message_update":
			// Reset progress activity timer - Pi is actively working
			lastProgressActivity = time.Now()

			// Extract text deltas
			if assistantEvent, ok := event["assistantMessageEvent"].(map[string]interface{}); ok {
				if eventType, ok := assistantEvent["type"].(string); ok && eventType == "text_delta" {
					if delta, ok := assistantEvent["delta"].(string); ok {
						outputBuilder.WriteString(delta)
					}
				}
			}

		case "error":
			result.Success = false
			result.ExitCode = 1
			if err, ok := event["error"].(map[string]interface{}); ok {
				if errMsg, ok := err["errorMessage"].(string); ok {
					result.Error = errMsg
				}
			}
			// Exit on error event
			goto done
		}
	}

done:

	// Also read stderr for any errors (non-blocking)
	stderrScanner := bufio.NewScanner(stderr)
	stderrScanner.Split(scanLines)
	var stderrOutput strings.Builder
	for stderrScanner.Scan() {
		line := stderrScanner.Text()
		if line != "" {
			stderrOutput.WriteString(line)
			stderrOutput.WriteString("\n")
		}
	}

	// If we never got agent_end and no error set, mark as failure
	if !agentEnded && result.Error == "" {
		result.Success = false
		result.ExitCode = 1
		result.Error = "agent did not complete (no agent_end event)"
	}

	result.Output = outputBuilder.String()
	if result.Error == "" && stderrOutput.Len() > 0 {
		result.Error = stderrOutput.String()
	}

	result.Duration = time.Since(startTime)
	return result
}

// scanLines is a custom split function for JSONL (handles \r\n properly)
func scanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	// Look for \n
	if i := strings.IndexByte(string(data), '\n'); i >= 0 {
		// We have a full line
		line := data[0:i]
		// Strip trailing \r if present
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[0 : len(line)-1]
		}
		return i + 1, line, nil
	}

	// If we're at EOF, return what we have
	if atEOF {
		return len(data), data, nil
	}

	// Request more data
	return 0, nil, nil
}

// Abort forcibly stops the Pi session and its process group
func (c *PiConnector) Abort(ctx context.Context, session *AgentSession) error {
	if session.PID == 0 {
		return fmt.Errorf("no process to abort")
	}

	// Kill the process group
	log.Printf("[PiConnector] Aborting session %s (PID: %d, PGID: %d)", session.ID, session.PID, -session.PID)

	// Try graceful termination of process group first
	if err := syscall.Kill(-session.PID, syscall.SIGTERM); err != nil {
		log.Printf("[PiConnector] Graceful kill of process group failed: %v", err)
		// Force kill the process group
		if err := syscall.Kill(-session.PID, syscall.SIGKILL); err != nil {
			log.Printf("[PiConnector] Force kill of process group failed: %v", err)
			// Fallback: kill just the main process
			exec.Command("kill", "-9", fmt.Sprintf("%d", session.PID)).Run()
		}
	}

	// Additional cleanup: kill any remaining children
	cleanupCmd := exec.Command("pkill", "-P", fmt.Sprintf("%d", session.PID))
	cleanupCmd.Run() // Ignore errors - best effort cleanup

	return nil
}

// Status returns current session status
func (c *PiConnector) Status(ctx context.Context, session *AgentSession) (AgentStatus, error) {
	// Since Pi RPC is synchronous, we check if process is still running
	if session.PID == 0 {
		return AgentStatus{State: AgentStateUnknown}, nil
	}

	// Check if process exists
	err := exec.Command("kill", "-0", fmt.Sprintf("%d", session.PID)).Run()
	if err != nil {
		// Process doesn't exist
		return AgentStatus{
			State:        AgentStateCompleted,
			LastActivity: time.Now(),
		}, nil
	}

	// Process is still running
	return AgentStatus{
		State:        AgentStateRunning,
		LastActivity: time.Now(),
	}, nil
}

// Cleanup performs session cleanup
func (c *PiConnector) Cleanup(ctx context.Context, session *AgentSession) error {
	// Abort if still running
	if session.PID > 0 {
		c.Abort(ctx, session)
	}

	log.Printf("[PiConnector] Cleaned up session %s", session.ID)
	return nil
}

// IsHealthy checks if Pi is available
func (c *PiConnector) IsHealthy() bool {
	return c.ensureInstalled() == nil
}

// CheckInstallation checks if Pi agent is installed and available in PATH
// Returns nil if pi is installed, error otherwise
// escapeForJSON escapes a string for safe inclusion in JSON
func escapeForJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func (c *PiConnector) CheckInstallation() error {
	c.logger.Printf(`{"event": "pi_installation_check", "message": "Checking Pi agent installation...", "timestamp": "%s"}`, time.Now().Format(time.RFC3339))

	// Check cached path first (with TTL)
	c.mu.RLock()
	if c.pathsCached && time.Since(c.pathsCacheTime) < installationCacheTTL && c.piPath != "" {
		c.mu.RUnlock()
		c.logger.Printf(`{"event": "pi_installation_check", "status": "found", "path": "%s", "cached": true, "timestamp": "%s"}`, c.piPath, time.Now().Format(time.RFC3339))
		return nil
	}
	c.mu.RUnlock()

	// Check if pi command exists
	piPath, err := c.executor.LookPath("pi")
	if err != nil {
		c.logger.Printf(`{"event": "pi_installation_check", "status": "not_found", "error": "%s", "timestamp": "%s"}`, err.Error(), time.Now().Format(time.RFC3339))
		return fmt.Errorf("pi command not found in PATH: %w", err)
	}

	// Cache the path
	c.mu.Lock()
	c.piPath = piPath
	c.pathsCached = true
	c.pathsCacheTime = time.Now()
	c.mu.Unlock()

	c.logger.Printf(`{"event": "pi_installation_check", "status": "found", "path": "%s", "timestamp": "%s"}`, piPath, time.Now().Format(time.RFC3339))
	return nil
}

// Install installs Pi agent via npm globally
// Returns error if npm is not available or installation fails
// Uses cached npm path when available to reduce PATH lookups
func (c *PiConnector) Install() error {
	c.logger.Printf(`{"event": "pi_installation_start", "message": "Installing Pi agent via npm...", "timestamp": "%s"}`, time.Now().Format(time.RFC3339))

	// Check cached npm path first (with TTL)
	var npmPath string
	var err error

	c.mu.RLock()
	if c.pathsCached && time.Since(c.pathsCacheTime) < installationCacheTTL && c.npmPath != "" {
		npmPath = c.npmPath
		c.mu.RUnlock()
		c.logger.Printf(`{"event": "pi_installation_step", "step": "npm_check", "status": "success", "npm_path": "%s", "cached": true, "timestamp": "%s"}`, npmPath, time.Now().Format(time.RFC3339))
	} else {
		c.mu.RUnlock()

		// Check if npm is available
		npmPath, err = c.executor.LookPath("npm")
		if err != nil {
			c.logger.Printf(`{"event": "pi_installation_error", "error": "npm not found in PATH", "details": "%s", "timestamp": "%s"}`, err.Error(), time.Now().Format(time.RFC3339))
			return fmt.Errorf("npm not found in PATH, cannot install pi. Please install Node.js and npm first: %w", err)
		}

		// Cache the npm path
		c.mu.Lock()
		c.npmPath = npmPath
		c.pathsCached = true
		c.pathsCacheTime = time.Now()
		c.mu.Unlock()

		c.logger.Printf(`{"event": "pi_installation_step", "step": "npm_check", "status": "success", "npm_path": "%s", "timestamp": "%s"}`, npmPath, time.Now().Format(time.RFC3339))
	}

	// Install Pi globally
	c.logger.Printf(`{"event": "pi_installation_step", "step": "npm_install", "message": "Running npm install -g @mariozechner/pi-coding-agent", "timestamp": "%s"}`, time.Now().Format(time.RFC3339))
	cmd := c.executor.Command("npm", "install", "-g", "@mariozechner/pi-coding-agent")
	output, err := cmd.CombinedOutput()

	if err != nil {
		outputStr := escapeForJSON(string(output))
		c.logger.Printf(`{"event": "pi_installation_error", "error": "%s", "npm_output": "%s", "timestamp": "%s"}`, err.Error(), outputStr, time.Now().Format(time.RFC3339))
		return fmt.Errorf("npm install failed: %w\nOutput: %s", err, string(output))
	}

	outputStr := escapeForJSON(string(output))
	c.logger.Printf(`{"event": "pi_installation_step", "step": "npm_install", "status": "success", "npm_output": "%s", "timestamp": "%s"}`, outputStr, time.Now().Format(time.RFC3339))

	// Verify installation
	c.logger.Printf(`{"event": "pi_installation_step", "step": "verification", "message": "Verifying Pi installation...", "timestamp": "%s"}`, time.Now().Format(time.RFC3339))
	if verifyErr := c.CheckInstallation(); verifyErr != nil {
		c.logger.Printf(`{"event": "pi_installation_error", "error": "verification failed", "details": "%s", "timestamp": "%s"}`, verifyErr.Error(), time.Now().Format(time.RFC3339))
		return fmt.Errorf("pi installation verification failed: %w", verifyErr)
	}

	c.logger.Printf(`{"event": "pi_installation_complete", "status": "success", "message": "Pi agent installed and verified successfully", "timestamp": "%s"}`, time.Now().Format(time.RFC3339))
	return nil
}

// ensureInstalled checks if Pi is installed, attempts installation if not
// This is the internal method used by Start() and IsHealthy()
// Uses TTL-based caching for performance: cached results return in <1ms
func (c *PiConnector) ensureInstalled() error {
	c.mu.RLock()
	// Check cached installation status with TTL
	if c.installCheck && time.Since(c.installCheckTime) < installationCacheTTL {
		installed := c.installed
		c.mu.RUnlock()
		if installed {
			return nil
		}
		return fmt.Errorf("pi not installed and installation failed")
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring lock
	if c.installCheck && time.Since(c.installCheckTime) < installationCacheTTL {
		if c.installed {
			return nil
		}
		return fmt.Errorf("pi not installed")
	}

	c.installCheck = true
	c.installCheckTime = time.Now()

	// Check if pi command exists using internal check to avoid lock reentrancy
	// Note: We hold the write lock, so we can't call CheckInstallation() which acquires its own locks
	piPath, lookErr := c.executor.LookPath("pi")
	if lookErr == nil {
		// Pi is installed - cache the path
		c.piPath = piPath
		c.pathsCached = true
		c.pathsCacheTime = time.Now()
		c.installed = true
		return nil
	}

	// Pi not found - try to install (release lock during install to avoid deadlock)
	c.mu.Unlock()
	installErr := c.Install()
	c.mu.Lock()

	if installErr != nil {
		c.installed = false
		return installErr
	}

	c.installed = true
	return nil
}

// enrichPromptWithHindsight queries Hindsight for relevant context and adds it to the prompt
func (c *PiConnector) enrichPromptWithHindsight(session *AgentSession, prompt string) string {
	// Check if Hindsight is enabled
	if os.Getenv("HYDRA_HISTORIESIGHT_ENABLED") != "true" {
		return prompt
	}

	// Get Hindsight config from environment
	baseURL := os.Getenv("HYDRA_HISTORIESIGHT_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8888"
	}

	timeout := 5 * time.Second
	if timeoutStr := os.Getenv("HYDRA_HISTORIESIGHT_TIMEOUT"); timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = d
		}
	}

	limit := 3
	if limitStr := os.Getenv("HYDRA_HISTORIESIGHT_LIMIT"); limitStr != "" {
		if n, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || n != 1 {
			limit = 3
		}
	}

	minScore := 0.7
	if scoreStr := os.Getenv("HYDRA_HISTORIESIGHT_MIN_SCORE"); scoreStr != "" {
		if n, err := fmt.Sscanf(scoreStr, "%f", &minScore); err != nil || n != 1 {
			minScore = 0.7
		}
	}

	// Create Hindsight client
	client := hindsight.NewClient(
		hindsight.WithBaseURL(baseURL),
		hindsight.WithTimeout(timeout),
	)

	if !client.IsEnabled() {
		return prompt
	}

	// Get bead info from session metadata
	beadTitle := ""
	beadType := ""
	if title, ok := session.metadata["bead_title"].(string); ok {
		beadTitle = title
	}
	if btype, ok := session.metadata["bead_type"].(string); ok {
		beadType = btype
	}

	log.Printf("[PiConnector] Querying Hindsight for context (bead: %s)...", session.BeadID)

	// Query Hindsight for relevant memories
	resp, err := client.SearchByBead(beadTitle, beadType)
	if err != nil {
		log.Printf("[PiConnector] Warning: failed to query Hindsight: %v", err)
		return prompt
	}

	if len(resp.Memories) == 0 {
		log.Printf("[PiConnector] No relevant memories found in Hindsight")
		return prompt
	}

	// Filter by score and limit. The recall API applies min_score
	// server-side and does not return a per-memory score, so treat a
	// zero/absent score as already-passed — only drop a memory when it
	// carries a real score below the threshold.
	var relevantMemories []hindsight.Memory
	for _, mem := range resp.Memories {
		hasScore := mem.Score > 0 || mem.Relevance > 0
		if hasScore && mem.Score < minScore && mem.Relevance < minScore {
			continue
		}
		relevantMemories = append(relevantMemories, mem)
		if len(relevantMemories) >= limit {
			break
		}
	}

	if len(relevantMemories) == 0 {
		log.Printf("[PiConnector] No relevant memories found in Hindsight (after filtering)")
		return prompt
	}

	log.Printf("[PiConnector] Found %d relevant memories in Hindsight", len(relevantMemories))

	// Build context section
	var contextParts []string
	contextParts = append(contextParts, "\n--- Relevant Context from Previous Work ---")
	for i, mem := range relevantMemories {
		contextParts = append(contextParts, fmt.Sprintf("%d. %s (relevance: %.2f)\n   %s",
			i+1, mem.ID, mem.Score, mem.Content))
	}
	contextParts = append(contextParts, "--- End Context ---\n")

	// Prepend context to prompt
	enhancedPrompt := strings.Join(contextParts, "\n") + "\n" + prompt

	return enhancedPrompt
}
