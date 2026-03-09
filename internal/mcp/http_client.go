package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// HTTPClient represents an MCP HTTP client for communicating with an MCP server.
// This implements the Streamable HTTP protocol used by MCP Agent Mail.
type HTTPClient struct {
	url            string
	agentID        string
	httpClient     *http.Client
	mu             sync.RWMutex
	connected      bool
	messageHandler func(Message)

	requestID int64
	requestMu sync.Mutex
}

// NewHTTPClient creates a new MCP HTTP client instance
func NewHTTPClient(url, agentID string) (*HTTPClient, error) {
	if url == "" {
		return nil, fmt.Errorf("URL cannot be empty")
	}
	if agentID == "" {
		return nil, fmt.Errorf("agentID cannot be empty")
	}

	return &HTTPClient{
		url:     url,
		agentID: agentID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		requestID: 0,
	}, nil
}

// SetMessageHandler sets the callback function for incoming messages
func (c *HTTPClient) SetMessageHandler(handler func(Message)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messageHandler = handler
}

// Connect verifies the MCP server is accessible
func (c *HTTPClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return fmt.Errorf("already connected")
	}

	// Test connection with health check
	resp, err := c.httpClient.Get(c.url + "/health/liveness")
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MCP server returned status %d", resp.StatusCode)
	}

	c.connected = true
	log.Printf("[MCP Client] Connected to %s as agent %s", c.url, c.agentID)
	return nil
}

// Disconnect marks the client as disconnected
func (c *HTTPClient) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	log.Printf("[MCP Client] Disconnected from %s", c.url)
	return nil
}

// IsConnected returns the connection status
func (c *HTTPClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// SendMessage sends a message to the MCP server via HTTP POST
func (c *HTTPClient) SendMessage(msg Message) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequest("POST", c.url+"/api", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", c.agentID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// SendJSONRPC sends a JSON-RPC message to the MCP server
func (c *HTTPClient) SendJSONRPC(method string, params interface{}) error {
	c.requestMu.Lock()
	c.requestID++
	id := c.requestID
	c.requestMu.Unlock()

	msg := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  mustMarshal(params),
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON-RPC message: %w", err)
	}

	req, err := http.NewRequest("POST", c.url+"/api", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", c.agentID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send JSON-RPC message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// mustMarshal marshals v to JSON, panicking on error (should never happen with valid types)
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal: %v", err))
	}
	return data
}

// Register sends a registration message to the MCP server
func (c *HTTPClient) Register(metadata RegistrationMetadata) error {
	return c.SendMessage(Message{
		Type: MsgTypeRegistration,
		Payload: map[string]interface{}{
			"agent_id":   metadata.AgentID,
			"bead_id":    metadata.BeadID,
			"type":       metadata.Type,
			"project":    metadata.Project,
			"repo":       metadata.Repo,
			"start_time": metadata.StartTime,
			"metadata":   metadata.Metadata,
		},
	})
}

// Deregister sends a deregistration message to the MCP server
func (c *HTTPClient) Deregister(metadata RegistrationMetadata) error {
	return c.SendMessage(Message{
		Type: MsgTypeDeregistration,
		Payload: map[string]interface{}{
			"agent_id": metadata.AgentID,
		},
	})
}

// Broadcast sends a message to all agents
func (c *HTTPClient) Broadcast(msgType string, content map[string]interface{}) error {
	return c.SendMessage(Message{
		Type:    msgType,
		Payload: content,
	})
}

// SendLog sends a log message to the MCP server
func (c *HTTPClient) SendLog(workerID string, level string, message string) error {
	return c.SendMessage(Message{
		Type: MsgTypeLog,
		Payload: map[string]interface{}{
			"worker_id": workerID,
			"level":     level,
			"message":   message,
		},
	})
}

// GetStatus returns the current connection status
func (c *HTTPClient) GetStatus() ClientStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ClientStatus{
		Connected: c.connected,
		URL:       c.url,
		AgentID:   c.agentID,
		Enabled:   true,
	}
}
