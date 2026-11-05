// Package hindsight provides integration with the Hindsight memory service
// for retrieving relevant historical context before worker execution.
package hindsight

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BankID is the identifier for the shared cross-project memory bank.
// All Hydra projects use this single "coding-brain" memory bank for knowledge sharing.
const BankID = "coding-brain"

// Memory represents a single memory entry from Hindsight.
// Each memory includes project attribution to enable cross-project knowledge sharing
type Memory struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Text       string   `json:"text"` // recall API returns the memory body under "text"
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
	BankID   string `json:"bank_id"`             // Memory bank identifier (always "coding-brain")
	WorkerID string `json:"worker_id,omitempty"` // Worker that created this memory
}

// SearchRequest represents a request to search Hindsight memories.
// Supports cross-project queries with project filtering and bank attribution.
// SearchRequest is the recall request body.
//
// IMPORTANT: the fields here must match what the Hindsight recall endpoint
// actually accepts. Verified against the live API's OpenAPI schema
// (v0.6.2, /v1/default/banks/{bank_id}/memories/recall), the accepted
// properties are: query, types, budget, max_tokens, trace, query_timestamp,
// include, tags, tags_match, tag_groups.
//
// This struct previously sent `limit`, `min_score`, `days_back` and `bank_id`
// in the body — none of which exist in the schema, so they were silently
// dropped. The effect was that every recall returned the ENTIRE bank (measured:
// 49 memories, ~80KB) and the client kept the first N with no quality
// filtering at all, while `min_score: 0.7` sat in the config implying a filter
// that never ran. `max_tokens` is the API's real knob and is a better fit than
// a count: it bounds the prompt cost directly. Measured effect on the live API:
// 80,517 B (limit/min_score) -> 15,736 B (max_tokens=800).
type SearchRequest struct {
	Query string `json:"query"`

	// MaxTokens bounds the size of the recalled context. This is the API's
	// supported way to limit results; a count-based limit is not.
	MaxTokens int `json:"max_tokens,omitempty"`

	// Budget controls recall depth server-side ("low"/"medium"/"high").
	// Valid per the recall API schema; "low" bounds candidates and cuts
	// rerank latency. Empty means the API default.
	Budget string `json:"budget,omitempty"`

	// Types optionally narrows which memory kinds are recalled (API field
	// name is plural "types", not "type").
	//
	// The only valid values are Hindsight's own memory kinds — measured against
	// the live bank: "experience", "observation", "world". These are NOT bead
	// types. Passing a bead type ("compliance", "GREEN", "REFACTOR", …) matches
	// zero memories and silently empties every recall; see the regression note
	// on Search. Leave unset unless you are filtering by a value from that list.
	Types []string `json:"types,omitempty"`

	// PreferObservations drops any raw fact (world/experience) that an
	// observation in THIS result set was consolidated from, backfilling the
	// freed slots with next results. After the 2026-11-04/05 backlog burn the
	// bank's duplicated clusters exist as (observation + N superseded raws);
	// without this flag recall serves all N copies. Scoped to the result set,
	// so a raw whose observation was not retrieved still serves — no
	// information cliff.
	PreferObservations bool `json:"prefer_observations,omitempty"`
}

// SearchResponse represents the response from Hindsight search
type SearchResponse struct {
	Success      bool     `json:"success"`
	Error        string   `json:"error,omitempty"`
	Count        int      `json:"count"`
	TotalMatches int      `json:"total_matches"`
	Memories     []Memory `json:"memories"`
	Results      []Memory `json:"results"` // recall API returns matches under "results"
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
	maxTokens         int
	budget            string
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

// WithBudget sets the recall budget ("low"/"medium"/"high") sent in the
// recall request body. Empty means the API default. "low" bounds candidates
// server-side, cutting rerank latency for daemon pre-queries.
func WithBudget(budget string) ClientOption {
	return func(c *Client) {
		c.budget = budget
	}
}

// WithDefaultLimit sets the default search limit
func WithDefaultLimit(limit int) ClientOption {
	return func(c *Client) {
		c.defaultLimit = limit
	}
}

// WithMaxTokens bounds the recalled context by token budget. This replaces the
// former WithMinScore: the recall API has no min_score field, so that setting
// was silently discarded (see SearchRequest).
func WithMaxTokens(maxTokens int) ClientOption {
	return func(c *Client) {
		if maxTokens > 0 {
			c.maxTokens = maxTokens
		}
	}
}

// WithEnabled sets whether the client is enabled
func WithEnabled(enabled bool) ClientOption {
	return func(c *Client) {
		c.enabled = enabled
	}
}

// defaultTimeout must exceed real recall latency or every caller that does not
// pass WithTimeout fails 100% of the time, silently — Search returns an error,
// the callers treat "no memories" as a normal empty result, and nothing logs a
// problem. Measured against the live service (bank hydra, max_tokens=800): 3.43s,
// 4.01s, 3.73s, 3.43s. The former 2s default sat below all four. Production
// config overrides this with 10s; the default now matches so a caller that
// forgets the option degrades to slow rather than to always-empty.
const defaultTimeout = 10 * time.Second

// NewClient creates a new Hindsight client with the specified options
func NewClient(opts ...ClientOption) *Client {
	client := &Client{
		baseURL:      "http://127.0.0.1:5000",
		timeout:      defaultTimeout,
		defaultLimit: 5,
		maxTokens:    800,
		enabled:      true,
		httpClient:   &http.Client{Timeout: defaultTimeout},
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
// Returns memories within the configured token budget.
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

	// Bound the recall by token budget — the API's supported control. The old
	// count/score fields were silently ignored, so every call pulled the whole
	// bank and the client threw most of it away.
	//
	// beadType is deliberately NOT sent as a types filter. It reads like the
	// obvious use for the field and it is wrong: Hindsight's types are memory
	// kinds ("experience", "observation", "world"), not bead types. Sending the
	// bead type matched nothing, and because the previous code sent the field
	// under the wrong name ("type"), the API ignored it and no one noticed —
	// fixing the name activated a filter that had never actually run. Measured
	// on the live bank: no filter -> 12 results; types:["compliance"] -> 0.
	// Deployed mid-run 2026-07-27, every worker went from "Retrieved 5 relevant
	// memories" to "No relevant memories found" — the recall half of the
	// learning loop, silently off. beadType still shapes the query text in
	// SearchByBead, which is where it belongs.
	reqBody := SearchRequest{
		Query:              query,
		MaxTokens:          c.maxTokens,
		Budget:             c.budget,
		PreferObservations: true,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	url := fmt.Sprintf("%s/v1/default/banks/%s/memories/recall", c.baseURL, BankID)
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

	// The recall API returns matches under "results" (not "memories"), puts
	// the memory body in "text" (not "content"), and — verified against the
	// live service — sends NO per-memory score and IGNORES both the "limit"
	// and "min_score" request fields (a query can return 60+ results
	// regardless). Results arrive server-ranked, so enforce the caller's
	// limit here by truncating to the top-k; without this every worker
	// prompt was enriched with the full unbounded result set.
	if len(result.Memories) == 0 && len(result.Results) > 0 {
		result.Memories = result.Results
	}
	if limit > 0 && len(result.Memories) > limit {
		result.Memories = result.Memories[:limit]
	}
	for i := range result.Memories {
		if result.Memories[i].Content == "" && result.Memories[i].Text != "" {
			result.Memories[i].Content = result.Memories[i].Text
		}
	}
	result.Count = len(result.Memories)
	result.Success = true

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
// Memories are stored in the shared "coding-brain" memory bank with project attribution.
type RetainRequest struct {
	Memory ExecutionMemory `json:"memory"`
	BankID string          `json:"bank_id"` // Target memory bank (always "coding-brain")
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

	// Transform ExecutionMemory into Hindsight API format
	reqBody := c.buildRetainRequest(memory)

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

	url := fmt.Sprintf("%s/v1/default/banks/%s/memories", c.baseURL, BankID)
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

// buildRetainRequest transforms ExecutionMemory into Hindsight API format
func (c *Client) buildRetainRequest(memory *ExecutionMemory) map[string]interface{} {
	// Build content from execution results
	content := fmt.Sprintf("Bead: %s\nTitle: %s\nType: %s\nSuccess: %v\nDuration: %v\nAttempts: %d\n\nOutput:\n%s",
		memory.BeadID,
		memory.BeadMetadata.Title,
		memory.BeadMetadata.Type,
		memory.ExecutionResult.Success,
		memory.ExecutionResult.Duration,
		memory.ExecutionResult.Attempts,
		memory.ExecutionResult.Output,
	)

	if memory.ExecutionResult.Error != "" {
		content += fmt.Sprintf("\n\nError:\n%s", memory.ExecutionResult.Error)
	}

	// Build metadata
	metadata := map[string]string{
		"bead_id":    string(memory.BeadID),
		"worker_id":  memory.WorkerID,
		"agent_type": memory.BeadMetadata.AgentType,
		"project":    memory.BeadMetadata.Project,
		"repo":       memory.BeadMetadata.Repo,
		"exit_code":  fmt.Sprintf("%d", memory.ExecutionResult.ExitCode),
		"success":    fmt.Sprintf("%v", memory.ExecutionResult.Success),
	}

	// Build tags from bead metadata
	tags := []string{"coding-brain", "execution", memory.BeadMetadata.Type}
	if memory.BeadMetadata.Project != "" {
		tags = append(tags, memory.BeadMetadata.Project)
	}

	return map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"content":     content,
				"context":     memory.BeadMetadata.Title,
				"document_id": string(memory.BeadID),
				"metadata":    metadata,
				"tags":        tags,
				"timestamp":   memory.RecordedAt.Format(time.RFC3339),
			},
		},
		"async": true,
	}
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

// Reflect generates insights by reflecting on existing memories in Hindsight.
// This implements the HindsightReflector interface for settled-state reflection.
//
// Parameters:
//   - beadID: The ID of the settled bead being reflected upon
//   - context: Map containing bead metadata (title, description, project, etc.)
//
// Returns:
//   - []ReflectionInsight: List of generated insights
//   - error: Error if reflection fails
func (c *Client) Reflect(beadID types.BeadID, reflectionCtx map[string]interface{}) ([]ReflectionInsight, error) {
	if !c.IsEnabled() {
		return nil, nil
	}

	// Build reflection query from context
	query := c.buildReflectionQuery(reflectionCtx)
	if query == "" {
		return nil, fmt.Errorf("cannot build reflection query from empty context")
	}

	// Create reflection request
	reqBody := ReflectRequest{
		Query:     query,
		Budget:    "low", // Use low budget for quick insights
		MaxTokens: 2048,
		Tags:      []string{"coding-brain", "execution"},
		TagsMatch: "any",
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal reflect request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	url := fmt.Sprintf("%s/v1/default/banks/%s/reflect", c.baseURL, BankID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create reflect request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("hindsight reflect timed out after %v", c.timeout)
		}
		return nil, fmt.Errorf("hindsight reflect request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hindsight reflect returned status %d", resp.StatusCode)
	}

	var result ReflectResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode hindsight reflect response: %w", err)
	}

	// Parse insights from reflection text
	insights := c.parseReflectionInsights(result, beadID)

	return insights, nil
}

// buildReflectionQuery constructs a reflection query from bead context
func (c *Client) buildReflectionQuery(ctx map[string]interface{}) string {
	parts := []string{}

	// Add bead title if available
	if title, ok := ctx["bead_title"].(string); ok && title != "" {
		parts = append(parts, fmt.Sprintf("Task: %s", title))
	}

	// Add bead description if available
	if desc, ok := ctx["bead_description"].(string); ok && desc != "" {
		parts = append(parts, fmt.Sprintf("Description: %s", desc))
	}

	// Add project context
	if project, ok := ctx["project"].(string); ok && project != "" {
		parts = append(parts, fmt.Sprintf("Project: %s", project))
	}

	// Add completion context
	if settledAt, ok := ctx["settled_at"].(time.Time); ok {
		parts = append(parts, fmt.Sprintf("Completed at: %s", settledAt.Format(time.RFC3339)))
	}

	// Add stability info
	if stability, ok := ctx["stability_score"].(float64); ok {
		parts = append(parts, fmt.Sprintf("Stability score: %.2f", stability))
	}

	// Add the core reflection prompt
	parts = append(parts, "\nBased on this completed task and historical memories, what key insights, patterns, or lessons learned can be extracted? Focus on:")
	parts = append(parts, "1. What made this task successful or challenging?")
	parts = append(parts, "2. What patterns from past executions apply here?")
	parts = append(parts, "3. What mental models or best practices emerge?")

	return strings.Join(parts, "\n")
}

// parseReflectionInsights extracts structured insights from reflection response
func (c *Client) parseReflectionInsights(resp ReflectResponse, beadID types.BeadID) []ReflectionInsight {
	var insights []ReflectionInsight

	// If we have structured output with insights, use that
	if resp.StructuredOutput != nil {
		if insightsData, ok := resp.StructuredOutput["insights"].([]interface{}); ok {
			for _, item := range insightsData {
				if insightMap, ok := item.(map[string]interface{}); ok {
					insight := ReflectionInsight{
						Content: getString(insightMap, "content"),
						Type:    getString(insightMap, "type"),
					}
					if tags, ok := insightMap["tags"].([]interface{}); ok {
						for _, tag := range tags {
							if tagStr, ok := tag.(string); ok {
								insight.Tags = append(insight.Tags, tagStr)
							}
						}
					}
					if insight.Content != "" {
						insights = append(insights, insight)
					}
				}
			}
		}
	}

	// If no structured insights, create one from the text response
	if len(insights) == 0 && resp.Text != "" {
		insights = append(insights, ReflectionInsight{
			Content: resp.Text,
			Type:    "reflection",
			Tags:    []string{"coding-brain", "insight", string(beadID)},
		})
	}

	return insights
}

// getString safely extracts a string value from a map
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

// MentalModel is a bank mental model: an LLM-synthesized living summary of
// the bank's memories for a fixed source_query, auto-refreshed after
// consolidation when its trigger is set. Empty Content means never refreshed.
type MentalModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// mentalModelsResponse mirrors GET /v1/default/banks/{bank}/mental-models.
type mentalModelsResponse struct {
	Items []MentalModel `json:"items"`
	Total int           `json:"total"`
}

// GetMentalModels lists the bank's mental models (cheap list read, no LLM).
// NOTE: the LIST endpoint projects `content` (and `trigger`) as empty — real
// content only comes from the per-model DETAIL endpoint — so this fetches the
// detail for each listed model (a handful of ~50ms GETs, still no LLM).
// Models with genuinely empty content (never refreshed) are filtered out.
func (c *Client) GetMentalModels(ctx context.Context) ([]MentalModel, error) {
	if !c.IsEnabled() {
		return nil, nil
	}

	url := fmt.Sprintf("%s/v1/default/banks/%s/mental-models", c.baseURL, BankID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create mental models request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mental models request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read mental models response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mental models request returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed mentalModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse mental models response: %w", err)
	}

	models := make([]MentalModel, 0, len(parsed.Items))
	for _, m := range parsed.Items {
		detail, err := c.getMentalModelDetail(ctx, m.ID)
		if err != nil {
			// fail-open per model: one bad detail must not drop the rest
				models = append(models, m) // keep whatever the list knew (likely empty content)
				continue
			}
		if strings.TrimSpace(detail.Content) != "" {
			models = append(models, detail)
		}
	}
	return models, nil
}

// getMentalModelDetail fetches one model with its full content.
func (c *Client) getMentalModelDetail(ctx context.Context, id string) (MentalModel, error) {
	url := fmt.Sprintf("%s/v1/default/banks/%s/mental-models/%s", c.baseURL, BankID, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return MentalModel{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return MentalModel{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MentalModel{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return MentalModel{}, fmt.Errorf("detail %s returned %d", id, resp.StatusCode)
	}
	var m MentalModel
	if err := json.Unmarshal(body, &m); err != nil {
		return MentalModel{}, err
	}
	return m, nil
}
