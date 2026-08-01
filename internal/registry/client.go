package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is the worker-side HTTP client for the daemon registry API.
// Every method is fail-open by contract: errors are returned for the caller
// to log; a missing or old daemon must never block bead execution.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a client for the daemon API base URL (e.g.
// "http://127.0.0.1:8089"). Empty baseURL yields a nil client: callers treat
// nil as "registry disabled".
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) post(path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.baseURL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("registry unavailable on daemon")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("daemon answered %d", resp.StatusCode)
	}
	return nil
}

// Report upserts the worker's current state.
func (c *Client) Report(rep WorkerReport) error {
	return c.post("/api/v1/workers/report", rep)
}

// Complete marks the worker terminal with its exit code.
func (c *Client) Complete(beadID string, exitCode int) error {
	return c.post("/api/v1/workers/completed", CompletionReport{BeadID: beadID, ExitCode: exitCode})
}

// PollCommands fetches and consumes pending commands for the worker.
func (c *Client) PollCommands(beadID string) ([]Command, error) {
	resp, err := c.http.Get(c.baseURL + "/api/v1/workers/commands?bead_id=" + beadID)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, fmt.Errorf("registry unavailable on daemon")
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("daemon answered %d", resp.StatusCode)
	}
	var body struct {
		Commands []Command `json:"commands"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Commands, nil
}
