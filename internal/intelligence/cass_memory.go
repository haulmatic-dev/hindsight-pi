package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CassMemoryClient provides an interface to the cass_memory (cm) CLI tool.
// It enables retrieval of contextual intelligence, pattern recording, and outcome tracking.
type CassMemoryClient struct {
	enabled bool
	timeout time.Duration
	cmdPath string
}

// CassContextResponse represents the structured response from cm context command
type CassContextResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    struct {
		RelevantBullets []Bullet  `json:"relevant_bullets"`
		AntiPatterns    []Bullet  `json:"anti_patterns"`
		HistorySnippets []Snippet `json:"history_snippets"`
	} `json:"data"`
}

// Bullet represents a single knowledge bullet from cass_memory
type Bullet struct {
	ID       string   `json:"id"`
	Content  string   `json:"content"`
	Category string   `json:"category"`
	Score    float64  `json:"score"`
	Tags     []string `json:"tags,omitempty"`
}

// Snippet represents a historical context snippet
type Snippet struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
}

// AddResponse represents the response from cm add command
type AddResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    struct {
		ID string `json:"id"`
	} `json:"data"`
}

// OutcomeResponse represents the response from cm outcome command
type OutcomeResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ClientOption is a functional option for configuring CassMemoryClient
type ClientOption func(*CassMemoryClient)

// WithTimeout sets the command execution timeout
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *CassMemoryClient) {
		c.timeout = timeout
	}
}

// WithCommandPath sets a custom path to the cm binary
func WithCommandPath(path string) ClientOption {
	return func(c *CassMemoryClient) {
		c.cmdPath = path
	}
}

// WithEnabled sets whether the client is enabled
func WithEnabled(enabled bool) ClientOption {
	return func(c *CassMemoryClient) {
		c.enabled = enabled
	}
}

// NewCassMemoryClient creates a new cass_memory client with the specified options.
// By default, it attempts to detect the cm binary and enables the client if found.
func NewCassMemoryClient(opts ...ClientOption) *CassMemoryClient {
	client := &CassMemoryClient{
		enabled: false,
		timeout: 10 * time.Second,
		cmdPath: "/Users/buddhi/.local/bin/cm",
	}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	// Auto-detect if cm is available unless explicitly disabled
	if client.enabled || client.cmdPath != "" {
		client.enabled = client.isAvailable()
	}

	return client
}

// IsEnabled returns whether the client is enabled and cm is available
func (c *CassMemoryClient) IsEnabled() bool {
	return c.enabled
}

// isAvailable checks if the cm binary exists and is executable
func (c *CassMemoryClient) isAvailable() bool {
	if c.cmdPath == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.cmdPath, "--version")
	err := cmd.Run()

	return err == nil
}

// execute runs a cm command with the given arguments and returns the output
func (c *CassMemoryClient) execute(args ...string) ([]byte, error) {
	if !c.enabled {
		return nil, fmt.Errorf("cass_memory client is not enabled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.cmdPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("cm command timed out after %v", c.timeout)
		}
		return nil, fmt.Errorf("cm command failed: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// Context retrieves contextual intelligence for a given task.
// It executes: cm context "<task>" --json --limit 10 --history 5 --days 30
//
// Returns context including relevant bullets, anti-patterns, and history snippets.
// If the client is disabled, returns an empty response without error.
func (c *CassMemoryClient) Context(task string) (*CassContextResponse, error) {
	if !c.enabled {
		// Return empty response for graceful degradation
		return &CassContextResponse{
			Success: true,
			Data: struct {
				RelevantBullets []Bullet  `json:"relevant_bullets"`
				AntiPatterns    []Bullet  `json:"anti_patterns"`
				HistorySnippets []Snippet `json:"history_snippets"`
			}{
				RelevantBullets: []Bullet{},
				AntiPatterns:    []Bullet{},
				HistorySnippets: []Snippet{},
			},
		}, nil
	}

	if strings.TrimSpace(task) == "" {
		return nil, fmt.Errorf("task cannot be empty")
	}

	output, err := c.execute(
		"context",
		task,
		"--json",
		"--limit", "10",
		"--history", "5",
		"--days", "30",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get context: %w", err)
	}

	var response CassContextResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse context response: %w", err)
	}

	if !response.Success && response.Error != "" {
		return &response, fmt.Errorf("cm context error: %s", response.Error)
	}

	return &response, nil
}

// AddPattern adds a new pattern to cass_memory.
// It executes: cm add "<pattern>" --category <category> --json
//
// The category should be a valid cass_memory category. Use GetCategoryForAgentType
// to map agent types to appropriate categories.
func (c *CassMemoryClient) AddPattern(pattern, category string) error {
	if !c.enabled {
		return fmt.Errorf("cass_memory client is not enabled")
	}

	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("pattern cannot be empty")
	}

	if strings.TrimSpace(category) == "" {
		return fmt.Errorf("category cannot be empty")
	}

	output, err := c.execute(
		"add",
		pattern,
		"--category", category,
		"--json",
	)
	if err != nil {
		return fmt.Errorf("failed to add pattern: %w", err)
	}

	var response AddResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("failed to parse add response: %w", err)
	}

	if !response.Success {
		if response.Error != "" {
			return fmt.Errorf("cm add error: %s", response.Error)
		}
		return fmt.Errorf("failed to add pattern: unknown error")
	}

	return nil
}

// RecordOutcome records the outcome of applying rules.
// It executes: cm outcome <status> <comma-separated-ids> --duration <seconds> --json
//
// This is a best-effort operation - errors are logged but not returned to avoid
// disrupting the main workflow. Use this for telemetry and learning purposes.
func (c *CassMemoryClient) RecordOutcome(status string, ruleIDs []string, duration time.Duration) error {
	if !c.enabled {
		// Silently succeed if disabled (best effort)
		return nil
	}

	if strings.TrimSpace(status) == "" {
		// Best effort: don't fail on invalid input
		return nil
	}

	if len(ruleIDs) == 0 {
		// Best effort: don't fail on empty rule IDs
		return nil
	}

	// Validate status
	validStatuses := map[string]bool{"success": true, "failure": true, "partial": true}
	if !validStatuses[status] {
		// Best effort: accept unknown statuses but log warning
		status = "unknown"
	}

	ruleIDsStr := strings.Join(ruleIDs, ",")
	durationSeconds := int(duration.Seconds())

	output, err := c.execute(
		"outcome",
		status,
		ruleIDsStr,
		"--duration", fmt.Sprintf("%d", durationSeconds),
		"--json",
	)
	if err != nil {
		// Best effort: don't propagate error
		return nil
	}

	var response OutcomeResponse
	if err := json.Unmarshal(output, &response); err != nil {
		// Best effort: don't propagate error
		return nil
	}

	// Best effort: don't check response.Success, just return nil
	return nil
}

// agentTypeToCategory maps agent types to cass_memory categories
var agentTypeToCategory = map[string]string{
	"orchestrator":        "orchestration",
	"test-specialist":     "testing",
	"code-reviewer":       "review",
	"documentation":       "documentation",
	"security-auditor":    "security",
	"performance-analyst": "performance",
	"refactoring":         "refactoring",
	"bug-fixer":           "debugging",
	"feature-developer":   "development",
	"devops":              "infrastructure",
	"database":            "database",
	"frontend":            "frontend",
	"backend":             "backend",
	"api-designer":        "api-design",
	"ai-ml":               "ai-ml",
}

// GetCategoryForAgentType maps an agent type to its corresponding cass_memory category.
// Returns the agent type as-is if no mapping exists.
//
// Example mappings:
//   - "orchestrator" → "orchestration"
//   - "test-specialist" → "testing"
//   - "code-reviewer" → "review"
func GetCategoryForAgentType(agentType string) string {
	if category, ok := agentTypeToCategory[agentType]; ok {
		return category
	}
	return agentType
}

// RegisterAgentTypeMapping registers a custom agent type to category mapping.
// This allows extending the default mappings at runtime.
func RegisterAgentTypeMapping(agentType, category string) {
	agentTypeToCategory[agentType] = category
}

// GetAvailableCategories returns a list of all available cass_memory categories
func GetAvailableCategories() []string {
	categories := make([]string, 0, len(agentTypeToCategory))
	seen := make(map[string]bool)

	for _, category := range agentTypeToCategory {
		if !seen[category] {
			seen[category] = true
			categories = append(categories, category)
		}
	}

	return categories
}

// GetAvailableAgentTypes returns a list of all registered agent types
func GetAvailableAgentTypes() []string {
	types := make([]string, 0, len(agentTypeToCategory))
	for agentType := range agentTypeToCategory {
		types = append(types, agentType)
	}
	return types
}
