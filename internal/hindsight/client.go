// Package hindsight provides integration with the Hindsight memory service
// for retrieving relevant historical context before worker execution.
package hindsight

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// BankID is the identifier for the shared cross-project memory bank.
// All Hydra projects use this single "hydra" memory bank for knowledge sharing.
const BankID = "hydra"

// Memory represents a single memory entry from Hindsight.
// Each memory includes project attribution to enable cross-project knowledge sharing
type Memory struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Type       string   `json:"type"`
	Agent      string   `json:"agent"`
	SourcePath string   `json:"source_path,omitempty"`
	LineNumber int      `json:"line_number,omitempty"`
	Score      float64  `json:"score"`
	Relevance  float64  `json:"relevance"`
	CreatedAt  int64    `json:"created_at"`
	Tags       []string `json:"tags,omitempty"`

	// Project attribution fields for cross-project memory bank
	Project  string `json:"project"`             // Project this memory belongs to
	Repo     string `json:"repo,omitempty"`      // Repository within the project
	BankID   string `json:"bank_id"`             // Memory bank identifier (always "hydra")
	WorkerID string `json:"worker_id,omitempty"` // Worker that created this memory
}

// SearchRequest represents a request to search Hindsight memories.
// Supports cross-project queries with project filtering and bank attribution.
type SearchRequest struct {
	Query      string   `json:"query"`
	Type       string   `json:"type,omitempty"`
	Limit      int      `json:"limit"`
	MinScore   float64  `json:"min_score"`
	AgentTypes []string `json:"agent_types,omitempty"`
	DaysBack   int      `json:"days_back,omitempty"`

	// Cross-project memory bank fields
	BankID          string   `json:"bank_id"`                    // Target memory bank (always "hydra")
	Projects        []string `json:"projects,omitempty"`         // Filter by specific projects (empty = all)
	ExcludeProjects []string `json:"exclude_projects,omitempty"` // Projects to exclude from results
}

// SearchResponse represents the response from Hindsight search
type SearchResponse struct {
	Success      bool     `json:"success"`
	Error        string   `json:"error,omitempty"`
	Count        int      `json:"count"`
	TotalMatches int      `json:"total_matches"`
	Memories     []Memory `json:"memories"`
}

// HealthStatus represents the health check result for the Hindsight service
type HealthStatus struct {
	Healthy   bool   `json:"healthy"`
	CheckedAt int64  `json:"checked_at"`
	Version   string `json:"version,omitempty"`
	Cached    bool   `json:"cached"`
}

// Operation represents the type of Hindsight operation being performed
type Operation string

const (
	OpSearch        Operation = "search"
	OpRetain        Operation = "retain"
	OpHealth        Operation = "health"
	OpRetainContext Operation = "retain_context"
)

// ErrorContext provides detailed context for debugging client errors
type ErrorContext struct {
	RequestID    string        `json:"request_id,omitempty"`
	Query        string        `json:"query,omitempty"`
	Duration     time.Duration `json:"duration,omitempty"`
	RetryCount   int           `json:"retry_count,omitempty"`
	Timestamp    time.Time     `json:"timestamp,omitempty"`
	ClientConfig ClientConfig  `json:"client_config,omitempty"`
}

// ClientConfig captures relevant client configuration for debugging
type ClientConfig struct {
	BaseURL      string        `json:"base_url"`
	Timeout      time.Duration `json:"timeout"`
	DefaultLimit int           `json:"default_limit"`
	MinScore     float64       `json:"min_score"`
	Enabled      bool          `json:"enabled"`
}

// ClientError represents an error that occurred during a Hindsight client operation.
// It provides detailed context about the operation, endpoint, status, and underlying cause.
type ClientError struct {
	Op       Operation     // The operation being performed (e.g., search, retain)
	Message  string        // Human-readable error message
	Endpoint string        // The endpoint that was called
	Status   int           // HTTP status code if applicable
	Cause    error         // The underlying error that caused this error
	Context  *ErrorContext // Additional context for debugging
}

// Error implements the error interface
func (e *ClientError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("hindsight %s failed: %s (endpoint: %s, status: %d): %v",
			e.Op, e.Message, e.Endpoint, e.Status, e.Cause)
	}
	return fmt.Sprintf("hindsight %s failed: %s (endpoint: %s, status: %d)",
		e.Op, e.Message, e.Endpoint, e.Status)
}

// Unwrap returns the underlying error for errors.Is and errors.As
func (e *ClientError) Unwrap() error {
	return e.Cause
}

// IsNotFound returns true if the error indicates a resource was not found
func (e *ClientError) IsNotFound() bool {
	return e.Status == http.StatusNotFound
}

// IsTimeout returns true if the error indicates a timeout
func (e *ClientError) IsTimeout() bool {
	if e.Cause != nil {
		return errors.Is(e.Cause, context.DeadlineExceeded)
	}
	return false
}

// IsTemporary returns true if the error might be transient and worth retrying
func (e *ClientError) IsTemporary() bool {
	return e.Status >= 500 || e.Status == http.StatusTooManyRequests || e.IsTimeout()
}

// Client provides an interface to the Hindsight memory service
type Client struct {
	baseURL           string
	httpClient        *http.Client
	timeout           time.Duration
	defaultLimit      int
	minScore          float64
	enabled           bool
	lastHealthCheck   time.Time
	healthCheckResult bool
	healthCheckTTL    time.Duration
}

// ClientOption is a functional option for configuring the Client
type ClientOption func(*Client)

// WithBaseURL sets the base URL for the Hindsight service
func WithBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithTimeout sets the HTTP timeout
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.timeout = timeout
		c.httpClient.Timeout = timeout
	}
}

// WithDefaultLimit sets the default search limit
func WithDefaultLimit(limit int) ClientOption {
	return func(c *Client) {
		c.defaultLimit = limit
	}
}

// WithMinScore sets the minimum relevance score threshold
func WithMinScore(score float64) ClientOption {
	return func(c *Client) {
		c.minScore = score
	}
}

// WithEnabled sets whether the client is enabled
func WithEnabled(enabled bool) ClientOption {
	return func(c *Client) {
		c.enabled = enabled
	}
}

// NewClient creates a new Hindsight client with the specified options
func NewClient(opts ...ClientOption) *Client {
	client := &Client{
		baseURL:      "http://127.0.0.1:5000",
		timeout:      2 * time.Second,
		defaultLimit: 5,
		minScore:     0.75,
		enabled:      true,
		httpClient:   &http.Client{Timeout: 2 * time.Second},
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// IsEnabled returns whether the client is enabled
func (c *Client) IsEnabled() bool {
	return c.enabled && c.baseURL != ""
}

// Search searches Hindsight for memories matching the query
// Returns top N memories with relevance >= minScore
func (c *Client) Search(query, beadType string, limit int) (*SearchResponse, error) {
	if !c.IsEnabled() {
		return &SearchResponse{
			Success:      true,
			Count:        0,
			TotalMatches: 0,
			Memories:     []Memory{},
		}, nil
	}

	if strings.TrimSpace(query) == "" {
		return &SearchResponse{
			Success:      true,
			Count:        0,
			TotalMatches: 0,
			Memories:     []Memory{},
		}, nil
	}

	if limit <= 0 {
		limit = c.defaultLimit
	}

	reqBody := SearchRequest{
		Query:    query,
		Type:     beadType,
		Limit:    limit,
		MinScore: c.minScore,
		DaysBack: 30,     // Search last 30 days by default
		BankID:   BankID, // Always use the shared "hydra" memory bank
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	url := fmt.Sprintf("%s/api/v1/search", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("hindsight search timed out after %v", c.timeout)
		}
		return nil, fmt.Errorf("hindsight request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hindsight returned status %d", resp.StatusCode)
	}

	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode hindsight response: %w", err)
	}

	return &result, nil
}

// SearchByBead searches for memories relevant to a bead
// Combines bead title and type for better matching
func (c *Client) SearchByBead(title, beadType string) (*SearchResponse, error) {
	// Build query from title and type
	query := title
	if beadType != "" {
		query = fmt.Sprintf("%s %s", title, beadType)
	}

	return c.Search(query, beadType, c.defaultLimit)
}

// RetainRequest represents a request to retain an execution memory.
// Memories are stored in the shared "hydra" memory bank with project attribution.
type RetainRequest struct {
	Memory ExecutionMemory `json:"memory"`
	BankID string          `json:"bank_id"` // Target memory bank (always "hydra")
}

// RetainResponse represents the response from the retain endpoint
type RetainResponse struct {
	Success  bool   `json:"success"`
	MemoryID string `json:"memory_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

// RetainContextResult stores a context processor result in Hindsight.
// Context processor results are retained with "forever" policy as part of
// institutional knowledge and provide traceability to the original bead.
// Returns the retention result with the assigned memory ID if successful.
func (c *Client) RetainContextResult(result *ContextProcessorResult) (*RetentionResult, error) {
	if !c.IsEnabled() {
		return &RetentionResult{
			Success: true,
		}, nil
	}

	if result == nil {
		return nil, &ClientError{
			Op:      "retain_context",
			Message: "context result cannot be nil",
		}
	}

	// Ensure retention policy is set to forever for institutional knowledge
	if result.RetentionPolicy == "" {
		result.RetentionPolicy = RetentionPolicyForever
	}

	// Convert to ExecutionMemory for storage (uses same retention endpoint)
	memory := result.ToExecutionMemory()

	// Add context-specific tags to the memory for filtering
	if memory.BeadMetadata.Labels == nil {
		memory.BeadMetadata.Labels = []string{}
	}
	memory.BeadMetadata.Labels = append(memory.BeadMetadata.Labels, "context-processor", string(result.RetentionPolicy))

	return c.Retain(memory)
}

// Retain stores an execution memory in Hindsight for future learning.
// It POSTs the ExecutionMemory to /api/v1/retain endpoint.
// Returns the retention result with the assigned memory ID if successful.
func (c *Client) Retain(memory *ExecutionMemory) (*RetentionResult, error) {
	if !c.IsEnabled() {
		return &RetentionResult{
			Success: true,
		}, nil
	}

	if memory == nil {
		return nil, &ClientError{
			Op:      "retain",
			Message: "memory cannot be nil",
		}
	}

	reqBody := RetainRequest{
		Memory: *memory,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &ClientError{
			Op:      "retain",
			Message: "failed to marshal retain request",
			Cause:   err,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	url := fmt.Sprintf("%s/api/v1/retain", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, &ClientError{
			Op:      "retain",
			Message: "failed to create request",
			Cause:   err,
		}
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, &ClientError{
				Op:      "retain",
				Message: fmt.Sprintf("retain request timed out after %v", c.timeout),
				Cause:   context.DeadlineExceeded,
				Status:  http.StatusRequestTimeout,
			}
		}
		return nil, &ClientError{
			Op:      "retain",
			Message: "retain request failed",
			Cause:   err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &ClientError{
			Op:      "retain",
			Message: fmt.Sprintf("unexpected status code: %d", resp.StatusCode),
			Status:  resp.StatusCode,
		}
	}

	var result RetainResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, &ClientError{
			Op:      "retain",
			Message: "failed to decode retain response",
			Cause:   err,
		}
	}

	return &RetentionResult{
		Success:  result.Success,
		MemoryID: result.MemoryID,
		Error:    result.Error,
	}, nil
}

// FilterByScore filters memories to only include those with score >= threshold
func FilterByScore(memories []Memory, threshold float64) []Memory {
	var filtered []Memory
	for _, m := range memories {
		if m.Score >= threshold || m.Relevance >= threshold {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// ToJSON converts memories to JSON string for environment variable
func ToJSON(memories []Memory) (string, error) {
	response := SearchResponse{
		Success:  true,
		Count:    len(memories),
		Memories: memories,
	}

	data, err := json.Marshal(response)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// TruncateContent truncates memory content to max length
func TruncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}

// TruncateMemories truncates all memory contents to max length
func TruncateMemories(memories []Memory, maxLen int) []Memory {
	truncated := make([]Memory, len(memories))
	for i, m := range memories {
		truncated[i] = m
		truncated[i].Content = TruncateContent(m.Content, maxLen)
	}
	return truncated
}
