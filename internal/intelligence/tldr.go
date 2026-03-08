package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultTLDRURL is the default URL for the TLDR daemon
const DefaultTLDRURL = "http://localhost:3000"

// RequestTimeout is the timeout for TLDR API requests
const RequestTimeout = 10 * time.Second

// Dependency represents a code dependency relationship
type Dependency struct {
	File     string `json:"file"`
	Function string `json:"function"`
	Line     int    `json:"line"`
}

// TLDRImpactResponse represents the response from the impact analysis endpoint
type TLDRImpactResponse struct {
	File   string `json:"file"`
	Impact struct {
		Callers []Dependency `json:"callers"`
		Callees []Dependency `json:"callees"`
	} `json:"impact"`
	Scope struct {
		Files    int  `json:"files"`
		Tokens   int  `json:"tokens"`
		Exceeded bool `json:"exceeded"`
	} `json:"scope"`
}

// TLDRSemanticResult represents a single semantic search result
type TLDRSemanticResult struct {
	File     string  `json:"file"`
	Function string  `json:"function"`
	Score    float64 `json:"score"`
	Context  string  `json:"context"`
}

// TLDRSemanticResponse represents the response from the semantic search endpoint
type TLDRSemanticResponse struct {
	Results []TLDRSemanticResult `json:"results"`
}

// TLDRFunctionInfo represents function information in context extraction
type TLDRFunctionInfo struct {
	Name       string `json:"name"`
	Signature  string `json:"signature"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	DocComment string `json:"doc_comment,omitempty"`
}

// TLDRImportInfo represents import information in context extraction
type TLDRImportInfo struct {
	Path  string `json:"path"`
	Alias string `json:"alias,omitempty"`
}

// TLDRExportInfo represents export information in context extraction
type TLDRExportInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// TLDRContextResponse represents the response from the context extraction endpoint
type TLDRContextResponse struct {
	Functions []TLDRFunctionInfo `json:"functions"`
	Imports   []TLDRImportInfo   `json:"imports"`
	Exports   []TLDRExportInfo   `json:"exports"`
}

// TLDRClient is an HTTP client for the TLDR daemon
type TLDRClient struct {
	baseURL string
	client  *http.Client
}

// NewTLDRClient creates a new TLDR client with the specified base URL
// If baseURL is empty, it defaults to http://localhost:3000
func NewTLDRClient(baseURL string) *TLDRClient {
	if baseURL == "" {
		baseURL = DefaultTLDRURL
	}

	return &TLDRClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: RequestTimeout,
		},
	}
}

// NewTLDRClientWithHTTP creates a new TLDR client with a custom HTTP client
func NewTLDRClientWithHTTP(baseURL string, client *http.Client) *TLDRClient {
	if baseURL == "" {
		baseURL = DefaultTLDRURL
	}

	return &TLDRClient{
		baseURL: baseURL,
		client:  client,
	}
}

// IsAvailable checks if the TLDR daemon is available
func (c *TLDRClient) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// ImpactAnalysis performs impact analysis on a file
// file: the path to the file to analyze
// depth: the depth of the analysis (1-5 recommended)
// Returns TLDRImpactResponse containing callers, callees, and scope information
func (c *TLDRClient) ImpactAnalysis(file string, depth int) (*TLDRImpactResponse, error) {
	if file == "" {
		return nil, fmt.Errorf("file path cannot be empty")
	}

	if depth < 1 {
		depth = 1
	}

	payload := map[string]interface{}{
		"file":  file,
		"depth": depth,
	}

	var response TLDRImpactResponse
	if err := c.postJSON("/impact", payload, &response); err != nil {
		return nil, fmt.Errorf("impact analysis failed: %w", err)
	}

	return &response, nil
}

// SemanticSearch performs semantic search across the codebase
// query: the natural language query to search for
// Returns TLDRSemanticResponse containing matching results with relevance scores
func (c *TLDRClient) SemanticSearch(query string) (*TLDRSemanticResponse, error) {
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	payload := map[string]interface{}{
		"query":       query,
		"max_results": 10,
	}

	var response TLDRSemanticResponse
	if err := c.postJSON("/semantic", payload, &response); err != nil {
		return nil, fmt.Errorf("semantic search failed: %w", err)
	}

	return &response, nil
}

// Context extracts structured context information from a file
// file: the path to the file to analyze
// Returns TLDRContextResponse containing functions, imports, and exports
func (c *TLDRClient) Context(file string) (*TLDRContextResponse, error) {
	if file == "" {
		return nil, fmt.Errorf("file path cannot be empty")
	}

	payload := map[string]interface{}{
		"file":  file,
		"depth": 2,
	}

	var response TLDRContextResponse
	if err := c.postJSON("/context", payload, &response); err != nil {
		return nil, fmt.Errorf("context extraction failed: %w", err)
	}

	return &response, nil
}

// postJSON performs a POST request with JSON body and parses JSON response
func (c *TLDRClient) postJSON(endpoint string, payload interface{}, response interface{}) error {
	url := c.baseURL + endpoint

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), RequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

// Close cleans up any resources used by the client
func (c *TLDRClient) Close() error {
	// The default http.Client doesn't require explicit cleanup,
	// but we provide this method for interface compatibility
	return nil
}
