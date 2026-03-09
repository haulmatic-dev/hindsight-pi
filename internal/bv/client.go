package bv

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CLIError represents an error from executing a CLI command.
type CLIError struct {
	Command string
	Args    []string
	Stdout  string
	Stderr  string
	Err     error
}

func (e *CLIError) Error() string {
	cmd := e.Command
	if len(e.Args) > 0 {
		cmd = cmd + " " + strings.Join(e.Args, " ")
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("%s failed", cmd))

	if e.Stdout != "" {
		parts = append(parts, fmt.Sprintf("stdout: %s", strings.TrimSpace(e.Stdout)))
	}

	if e.Stderr != "" {
		parts = append(parts, fmt.Sprintf("stderr: %s", strings.TrimSpace(e.Stderr)))
	}

	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}

	return strings.Join(parts, ": ")
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

// CLIExecutor abstracts command execution for testability
type CLIExecutor interface {
	Execute(name string, arg ...string) ([]byte, error)
}

// defaultExecutor implements CLIExecutor using os/exec
type defaultExecutor struct{}

func (e *defaultExecutor) Execute(name string, arg ...string) ([]byte, error) {
	return exec.Command(name, arg...).Output()
}

// defaultExecutorWithDir executes commands in a specific directory
type defaultExecutorWithDir struct {
	Dir string
}

func (e *defaultExecutorWithDir) Execute(name string, arg ...string) ([]byte, error) {
	cmd := exec.Command(name, arg...)
	cmd.Dir = e.Dir
	return cmd.Output()
}

// Client wraps the bv CLI commands with graceful degradation
type Client struct {
	executor          CLIExecutor
	enabled           bool
	lastHealthCheck   time.Time
	healthCheckResult bool
	healthCheckTTL    time.Duration
}

// ClientOption configures the Client
type ClientOption func(*Client)

// WithExecutor sets a custom CLI executor
func WithExecutor(executor CLIExecutor) ClientOption {
	return func(c *Client) {
		c.executor = executor
	}
}

// WithHealthCheckTTL sets the TTL for health check caching
func WithHealthCheckTTL(ttl time.Duration) ClientOption {
	return func(c *Client) {
		c.healthCheckTTL = ttl
	}
}

// NewClient creates a new BV client with graceful degradation support
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		executor:       &defaultExecutor{},
		enabled:        true,
		healthCheckTTL: 30 * time.Second,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// exec executes a CLI command and returns the output or a CLIError
func (c *Client) exec(cmd string, args ...string) ([]byte, error) {
	output, err := c.executor.Execute(cmd, args...)
	if err != nil {
		return nil, &CLIError{
			Command: cmd,
			Args:    args,
			Err:     err,
		}
	}
	return output, nil
}

// execInDir executes a CLI command in a specific directory
func (c *Client) execInDir(dir string, cmd string, args ...string) ([]byte, error) {
	execCmd := exec.Command(cmd, args...)
	execCmd.Dir = dir
	output, err := execCmd.Output()
	if err != nil {
		return nil, &CLIError{
			Command: cmd,
			Args:    args,
			Err:     err,
		}
	}
	return output, nil
}

// isAvailable checks if bv CLI is available without caching
func (c *Client) isAvailable() bool {
	if !c.enabled {
		return false
	}
	_, err := c.exec("bv", "-version")
	return err == nil
}

// HealthCheck performs a health check and returns the status
func (c *Client) HealthCheck() HealthStatus {
	// Check if cached result is still valid
	if time.Since(c.lastHealthCheck) < c.healthCheckTTL {
		return HealthStatus{
			Healthy:   c.healthCheckResult,
			CheckedAt: c.lastHealthCheck,
			Cached:    true,
		}
	}

	// Perform fresh health check
	healthy := c.isAvailable()
	c.healthCheckResult = healthy
	c.lastHealthCheck = time.Now()

	return HealthStatus{
		Healthy:   healthy,
		CheckedAt: c.lastHealthCheck,
		Cached:    false,
	}
}

// IsHealthy returns true if bv is available (uses cached result if valid)
func (c *Client) IsHealthy() bool {
	status := c.HealthCheck()
	return status.Healthy
}

// executeRobotCommand executes a bv robot command with graceful degradation.
// This helper eliminates duplication across all Get* methods.
// If bv is unavailable, it returns an empty result (zero value) and no error.
func executeRobotCommand[T any](c *Client, projectPath string, robotFlag string, extraArgs []string) (*T, error) {
	if !c.IsHealthy() {
		return new(T), nil // Graceful degradation: return zero value
	}

	args := []string{robotFlag, "--format", "json"}
	args = append(args, extraArgs...)

	var output []byte
	var err error

	if projectPath != "" {
		output, err = c.execInDir(projectPath, "bv", args...)
	} else {
		output, err = c.exec("bv", args...)
	}

	if err != nil {
		if !c.isAvailable() {
			c.enabled = false
			return new(T), nil // Graceful degradation
		}
		return nil, err
	}

	var result T
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse %s JSON: %w", robotFlag, err)
	}

	return &result, nil
}

// GetInsights retrieves graph analysis and insights
func (c *Client) GetInsights(projectPath string) (*InsightsResponse, error) {
	return executeRobotCommand[InsightsResponse](c, projectPath, "--robot-insights", nil)
}

// GetTriage retrieves unified triage recommendations
func (c *Client) GetTriage(projectPath string) (*TriageResponse, error) {
	return executeRobotCommand[TriageResponse](c, projectPath, "--robot-triage", nil)
}

// GetPlan retrieves dependency-respecting execution plan
func (c *Client) GetPlan(projectPath string) (*PlanResponse, error) {
	return executeRobotCommand[PlanResponse](c, projectPath, "--robot-plan", nil)
}

// GetAlerts retrieves alerts (drift + proactive)
func (c *Client) GetAlerts(projectPath string) (*AlertsResponse, error) {
	return executeRobotCommand[AlertsResponse](c, projectPath, "--robot-alerts", nil)
}

// GetPriority retrieves priority recommendations
func (c *Client) GetPriority(projectPath string) (*PriorityResponse, error) {
	return executeRobotCommand[PriorityResponse](c, projectPath, "--robot-priority", nil)
}

// GetCapacity retrieves capacity simulation and completion projection
func (c *Client) GetCapacity(projectPath string, agents int) (*CapacityResponse, error) {
	var extraArgs []string
	if agents > 0 {
		extraArgs = []string{"--agents", fmt.Sprintf("%d", agents)}
	}
	return executeRobotCommand[CapacityResponse](c, projectPath, "--robot-capacity", extraArgs)
}

// GetFileHotspots retrieves files touched by most beads
func (c *Client) GetFileHotspots(projectPath string, limit int) (*FileHotspotsResponse, error) {
	var extraArgs []string
	if limit > 0 {
		extraArgs = []string{"--hotspots-limit", fmt.Sprintf("%d", limit)}
	}
	return executeRobotCommand[FileHotspotsResponse](c, projectPath, "--robot-file-hotspots", extraArgs)
}

// GetDrift checks for drift from baseline
func (c *Client) GetDrift(projectPath string) (*DriftResponse, error) {
	return executeRobotCommand[DriftResponse](c, projectPath, "--robot-drift", nil)
}

// Search performs semantic search
func (c *Client) Search(projectPath string, query string, limit int) (*SearchResponse, error) {
	if query == "" {
		return &SearchResponse{}, nil
	}

	extraArgs := []string{"--search", query}
	if limit > 0 {
		extraArgs = append(extraArgs, "--search-limit", fmt.Sprintf("%d", limit))
	}
	return executeRobotCommand[SearchResponse](c, projectPath, "--robot-search", extraArgs)
}

// GetRelatedBeads retrieves beads related to a specific bead
func (c *Client) GetRelatedBeads(projectPath string, beadID string) (*RelatedBeadsResponse, error) {
	if beadID == "" {
		return &RelatedBeadsResponse{}, nil
	}

	extraArgs := []string{beadID}
	return executeRobotCommand[RelatedBeadsResponse](c, projectPath, "--robot-related", extraArgs)
}

// GetImpactNetwork retrieves bead impact network
func (c *Client) GetImpactNetwork(projectPath string, beadID string) (*ImpactNetworkResponse, error) {
	var extraArgs []string
	if beadID != "" {
		extraArgs = []string{beadID}
	}
	return executeRobotCommand[ImpactNetworkResponse](c, projectPath, "--robot-impact-network", extraArgs)
}
