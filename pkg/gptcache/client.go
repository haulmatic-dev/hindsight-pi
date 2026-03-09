package gptcache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CacheResult represents the result of a cache lookup
type CacheResult struct {
	Answer     string  `json:"answer"`
	CacheHit   bool    `json:"cache_hit"`
	Similarity float64 `json:"similarity"`
}

// getRequest represents the request body for the /get endpoint
type getRequest struct {
	Prompt string `json:"prompt"`
}

// putRequest represents the request body for the /put endpoint
type putRequest struct {
	Prompt string `json:"prompt"`
	Answer string `json:"answer"`
}

// Client is an HTTP client for interacting with a GPTCache server
type Client struct {
	baseURL string
	enabled bool
	client  *http.Client
}

// NewClient creates a new GPTCache HTTP client
func NewClient(serverURL string, enabled bool) *Client {
	return &Client{
		baseURL: serverURL,
		enabled: enabled,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Get retrieves a cached response for the given prompt
// Returns nil, nil if the prompt is not in the cache (cache miss)
// Returns nil, error if the request fails
func (c *Client) Get(prompt string) (*CacheResult, error) {
	// Check if caching is enabled
	if !c.enabled {
		return nil, nil
	}

	// Prepare request body
	reqBody := getRequest{Prompt: prompt}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal get request: %w", err)
	}

	// Create request with context for timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/get", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create get request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		// Graceful degradation - return nil on network errors
		return nil, nil
	}
	defer resp.Body.Close()

	// Handle response
	switch resp.StatusCode {
	case http.StatusNotFound:
		// Cache miss - return nil, nil
		return nil, nil
	case http.StatusOK:
		// Cache hit - parse response
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read get response body: %w", err)
		}

		var result CacheResult
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal get response: %w", err)
		}

		return &result, nil
	default:
		// Unexpected status code
		return nil, fmt.Errorf("unexpected status code from cache get: %d", resp.StatusCode)
	}
}

// Put stores a prompt and answer in the cache
// Returns an error only if the request fails in a way that should be handled
// This is a best-effort operation - errors are logged but not propagated as failures
func (c *Client) Put(prompt, answer string) error {
	// Check if caching is enabled
	if !c.enabled {
		return nil
	}

	// Prepare request body
	reqBody := putRequest{
		Prompt: prompt,
		Answer: answer,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		// Best effort - log error but don't fail
		return nil
	}

	// Create request with context for timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/put", bytes.NewBuffer(jsonBody))
	if err != nil {
		// Best effort - log error but don't fail
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		// Best effort - log error but don't fail (graceful degradation)
		return nil
	}
	defer resp.Body.Close()

	// Check for non-success status codes
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// Best effort - log error but don't fail
		return nil
	}

	return nil
}
