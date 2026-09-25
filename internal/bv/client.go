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
	// If the executor supports directory-aware execution, use it (test doubles
	// implement ExecuteInDir; mirrors the beads client).
	if execWithDir, ok := c.executor.(interface {
		ExecuteInDir(dir string, name string, arg ...string) ([]byte, error)
	}); ok {
		return execWithDir.ExecuteInDir(dir, cmd, args...)
	}
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
	wrapper, err := executeRobotCommand[TriageWrapper](c, projectPath, "--robot-triage", nil)
	if err != nil {
		return nil, err
	}
	return &wrapper.Triage, nil
}

// GetPlan retrieves dependency-respecting execution plan.
// Returns the inner PlanData (tracks, actionable counts, summary).
func (c *Client) GetPlan(projectPath string) (*PlanData, error) {
	wrapper, err := executeRobotCommand[PlanWrapper](c, projectPath, "--robot-plan", nil)
	if err != nil {
		return nil, err
	}
	return &wrapper.Plan, nil
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

// graphEdge is one edge in bv --robot-graph adjacency output.
type graphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// robotGraphResponse mirrors bv --robot-graph JSON (adjacency.nodes/edges).
type robotGraphResponse struct {
	Adjacency struct {
		Edges []graphEdge `json:"edges"`
	} `json:"adjacency"`
}

// RobotGraphEdges returns the dependency map for a repo in ONE bv call
// (dependent -> blockers), replacing the per-bead `br show` N+1 pattern that
// contended on the br store during DAG rebuilds (2026-09-25: 43 gate edges
// silently dropped when br show failed under lock contention).
//
// bv edge semantics: {from: X, to: Y, type: "blocks"} means Y blocks X, i.e.
// X depends on Y. Only "blocks" edges constrain scheduling; other types
// (parent-child, discovered-from) are structural, not ordering constraints.
//
// Unlike the other robot commands this does NOT degrade to an empty success:
// an empty edge map is a valid graph state, so callers cannot distinguish
// "no edges" from "bv broken". Error propagates; caller decides fail policy.
func (c *Client) RobotGraphEdges(projectPath string) (map[string][]string, error) {
	if !c.IsHealthy() {
		return nil, fmt.Errorf("bv unhealthy")
	}

	var output []byte
	var err error
	if projectPath != "" {
		output, err = c.execInDir(projectPath, "bv", "--robot-graph", "--format", "json")
	} else {
		output, err = c.exec("bv", "--robot-graph", "--format", "json")
	}
	if err != nil {
		return nil, fmt.Errorf("bv --robot-graph failed: %w", err)
	}

	var resp robotGraphResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse --robot-graph JSON: %w", err)
	}

	deps := make(map[string][]string, len(resp.Adjacency.Edges))
	for _, e := range resp.Adjacency.Edges {
		if e.Type != "blocks" {
			continue
		}
		deps[e.From] = append(deps[e.From], e.To)
	}
	return deps, nil
}

// GetImpactNetwork retrieves bead impact network
func (c *Client) GetImpactNetwork(projectPath string, beadID string) (*ImpactNetworkResponse, error) {
	var extraArgs []string
	if beadID != "" {
		extraArgs = []string{beadID}
	}
	return executeRobotCommand[ImpactNetworkResponse](c, projectPath, "--robot-impact-network", extraArgs)
}
