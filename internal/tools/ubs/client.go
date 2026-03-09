package ubs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CLIExecutor abstracts command execution for testability
type CLIExecutor interface {
	Execute(name string, arg ...string) ([]byte, error)
	ExecuteContext(ctx context.Context, name string, arg ...string) ([]byte, error)
}

// DefaultExecutor implements CLIExecutor using exec.Command
type DefaultExecutor struct{}

func (e *DefaultExecutor) Execute(name string, arg ...string) ([]byte, error) {
	return exec.Command(name, arg...).CombinedOutput()
}

func (e *DefaultExecutor) ExecuteContext(ctx context.Context, name string, arg ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	return cmd.CombinedOutput()
}

// Client wraps the ubs CLI commands with graceful degradation support
type Client struct {
	executor          CLIExecutor
	enabled           bool
	command           string
	timeout           time.Duration
	lastHealthCheck   time.Time
	healthCheckResult bool
	healthCheckTTL    time.Duration
}

// ClientOption configures the Client
type ClientOption func(*Client)

// NewClient creates a new UBS client with sensible defaults
func NewClient(opts ...ClientOption) *Client {
	client := &Client{
		executor:       &DefaultExecutor{},
		enabled:        true,
		command:        "ubs",
		timeout:        30 * time.Second,
		healthCheckTTL: 5 * time.Minute,
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// WithExecutor sets a custom CLI executor (useful for testing)
func WithExecutor(executor CLIExecutor) ClientOption {
	return func(c *Client) {
		c.executor = executor
	}
}

// WithCommand sets the ubs command path
func WithCommand(cmd string) ClientOption {
	return func(c *Client) {
		c.command = cmd
	}
}

// WithTimeout sets the scan timeout
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// WithHealthCheckTTL sets the health check TTL
func WithHealthCheckTTL(ttl time.Duration) ClientOption {
	return func(c *Client) {
		c.healthCheckTTL = ttl
	}
}

// WithEnabled sets whether the client is enabled
func WithEnabled(enabled bool) ClientOption {
	return func(c *Client) {
		c.enabled = enabled
	}
}

// HealthCheck performs a health check and returns the status
func (c *Client) HealthCheck() HealthStatus {
	// Check if we have a cached result that's still valid
	if time.Since(c.lastHealthCheck) < c.healthCheckTTL {
		return HealthStatus{
			Healthy:   c.healthCheckResult,
			CheckedAt: c.lastHealthCheck.Unix(),
			Cached:    true,
		}
	}

	// Perform actual health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := c.executor.ExecuteContext(ctx, c.command, "--version")

	status := HealthStatus{
		CheckedAt: time.Now().Unix(),
		Cached:    false,
	}

	if err == nil {
		c.healthCheckResult = true
		status.Healthy = true
		status.Version = strings.TrimSpace(string(output))
	} else {
		c.healthCheckResult = false
		status.Healthy = false
	}

	c.lastHealthCheck = time.Now()
	return status
}

// IsHealthy returns true if ubs is available (uses cached health check)
func (c *Client) IsHealthy() bool {
	if !c.enabled {
		return false
	}

	// Check cache first
	if time.Since(c.lastHealthCheck) < c.healthCheckTTL {
		return c.healthCheckResult
	}

	// Perform fresh health check
	status := c.HealthCheck()
	return status.Healthy
}

// IsAvailable checks if ubs CLI is available (alias for IsHealthy)
func (c *Client) IsAvailable() bool {
	return c.IsHealthy()
}

// Scan performs a scan based on the provided options
func (c *Client) Scan(projectPath string, opts ScanOptions) (*UBSScanResult, error) {
	if !c.enabled {
		return nil, fmt.Errorf("ubs client is disabled")
	}

	// Build scan arguments based on options
	args := c.buildScanArgs(opts)

	return c.executeScan(projectPath, args)
}

// ScanFiles scans specific files
func (c *Client) ScanFiles(projectPath string, files []string, minSeverity string) (*UBSScanResult, error) {
	opts := ScanOptions{
		Files:       files,
		MinSeverity: minSeverity,
	}
	return c.Scan(projectPath, opts)
}

// ScanStaged scans only staged files
func (c *Client) ScanStaged(projectPath string, minSeverity string) (*UBSScanResult, error) {
	opts := ScanOptions{
		Staged:      true,
		MinSeverity: minSeverity,
	}
	return c.Scan(projectPath, opts)
}

// ScanDiff scans changed files vs HEAD
func (c *Client) ScanDiff(projectPath string, minSeverity string) (*UBSScanResult, error) {
	opts := ScanOptions{
		Diff:        true,
		MinSeverity: minSeverity,
	}
	return c.Scan(projectPath, opts)
}

// buildScanArgs builds the command arguments for a scan
func (c *Client) buildScanArgs(opts ScanOptions) []string {
	args := []string{"scan", "--format=json"}

	if opts.Staged {
		args = append(args, "--staged")
	}

	if opts.Diff {
		args = append(args, "--diff")
	}

	if opts.MinSeverity != "" {
		args = append(args, "--min-severity", opts.MinSeverity)
	}

	// Add specific files if provided
	args = append(args, opts.Files...)

	return args
}

// executeScan runs the ubs scan and parses the result
func (c *Client) executeScan(projectPath string, args []string) (*UBSScanResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	output, err := c.executor.ExecuteContext(ctx, c.command, args...)
	if err != nil {
		// Check if it's a timeout
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("ubs scan timed out after %v: %w", c.timeout, err)
		}
		return nil, fmt.Errorf("ubs scan failed: %w\nOutput: %s", err, string(output))
	}

	// Parse JSON output
	var result UBSScanResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse ubs output: %w\nOutput: %s", err, string(output))
	}

	return &result, nil
}

// ScanAndEvaluate performs a scan and evaluates against a quality threshold
func (c *Client) ScanAndEvaluate(projectPath string, opts ScanOptions, threshold QualityThreshold) (*UBSScanResult, bool, error) {
	result, err := c.Scan(projectPath, opts)
	if err != nil {
		return nil, false, err
	}

	passed := result.PassesQualityGate(threshold)
	return result, passed, nil
}

// GetCriticalBugs returns only critical bugs from a scan result
func (c *Client) GetCriticalBugs(result *UBSScanResult) []Bug {
	if result == nil {
		return nil
	}
	return result.GetBugsBySeverity(SeverityCritical)
}

// GetWarningBugs returns only warning bugs from a scan result
func (c *Client) GetWarningBugs(result *UBSScanResult) []Bug {
	if result == nil {
		return nil
	}
	return result.GetBugsBySeverity(SeverityWarning)
}

// FormatBugReport formats bugs into a human-readable report
func (c *Client) FormatBugReport(result *UBSScanResult) string {
	if result == nil || result.BugsFound == 0 {
		return "No bugs found."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d bug(s) in %d file(s)\n", result.BugsFound, result.FilesScanned))
	sb.WriteString(fmt.Sprintf("Quality Score: %.1f/100\n\n", result.Score))

	for _, bug := range result.Bugs {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", bug.Severity, bug.Rule))
		sb.WriteString(fmt.Sprintf("  File: %s:%d:%d\n", bug.File, bug.Line, bug.Column))
		sb.WriteString(fmt.Sprintf("  Message: %s\n\n", bug.Message))
	}

	return sb.String()
}
