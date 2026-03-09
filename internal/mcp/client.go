package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents an MCP WebSocket client for communicating with an MCP server.
type Client struct {
	url       string
	agentID   string
	conn      *websocket.Conn
	mu        sync.RWMutex
	msgChan   chan Message
	connected bool

	stopChan      chan struct{}
	reconnectChan chan struct{}

	reconnectMaxAttempts int
	reconnectBaseDelay   time.Duration
	reconnectMaxDelay    time.Duration
	heartbeatInterval    time.Duration
	writeTimeout         time.Duration
	dialer               *websocket.Dialer

	messageHandler func(Message)

	requestID int64
	requestMu sync.Mutex
	pending   map[interface{}]chan *JSONRPCMessage
}

// JSONRPCMessage represents a JSON-RPC 2.0 message for internal communication
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// NewClient creates a new MCP client instance
func NewClient(url, agentID string) (*Client, error) {
	if url == "" {
		return nil, errors.New("URL cannot be empty")
	}
	if agentID == "" {
		return nil, errors.New("agentID cannot be empty")
	}

	return &Client{
		url:                  url,
		agentID:              agentID,
		msgChan:              make(chan Message, 100),
		stopChan:             make(chan struct{}),
		reconnectChan:        make(chan struct{}, 1),
		reconnectMaxAttempts: 10,
		reconnectBaseDelay:   1 * time.Second,
		reconnectMaxDelay:    60 * time.Second,
		heartbeatInterval:    30 * time.Second,
		writeTimeout:         10 * time.Second,
		dialer: &websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
			Proxy:            http.ProxyFromEnvironment,
		},
		requestID: 0,
		pending:   make(map[interface{}]chan *JSONRPCMessage),
	}, nil
}

// SetMessageHandler sets the callback function for incoming messages
func (c *Client) SetMessageHandler(handler func(Message)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messageHandler = handler
}

// Connect establishes a WebSocket connection
func (c *Client) Connect() error {
	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return errors.New("already connected")
	}
	c.mu.Unlock()

	headers := http.Header{}
	headers.Set("X-Agent-ID", c.agentID)

	conn, resp, err := c.dialer.Dial(c.url, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	go c.messageListener()
	go c.heartbeat()
	go c.reconnectMonitor()

	log.Printf("[MCP Client] Connected to %s as agent %s", c.url, c.agentID)
	return nil
}

// Disconnect closes the connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil
	}

	conn := c.conn
	c.connected = false
	c.mu.Unlock()

	close(c.stopChan)

	if conn != nil {
		deadline := time.Now().Add(c.writeTimeout)
		conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			deadline)
		conn.Close()
	}

	close(c.msgChan)

	c.requestMu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.requestMu.Unlock()

	log.Printf("[MCP Client] Disconnected from %s", c.url)
	return nil
}

// IsConnected returns connection status
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Register sends agent registration
func (c *Client) Register(metadata RegistrationMetadata) error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	params := map[string]interface{}{
		"agent_id":   metadata.AgentID,
		"bead_id":    metadata.BeadID,
		"type":       metadata.Type,
		"project":    metadata.Project,
		"start_time": metadata.StartTime,
	}

	if metadata.Repo != "" {
		params["repo"] = metadata.Repo
	}
	if len(metadata.Metadata) > 0 {
		params["metadata"] = metadata.Metadata
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	response, err := c.sendJSONRPCRequest(ctx, "agent.register", params)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	if response.Error != nil {
		return fmt.Errorf("registration failed: %s", response.Error.Error())
	}

	log.Printf("[MCP Client] Registered agent: %s", metadata.AgentID)
	return nil
}

// Deregister removes agent registration
func (c *Client) Deregister() error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	params := map[string]interface{}{"agent_id": c.agentID}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	response, err := c.sendJSONRPCRequest(ctx, "agent.deregister", params)
	if err != nil {
		return fmt.Errorf("deregistration failed: %w", err)
	}

	if response.Error != nil {
		return fmt.Errorf("deregistration failed: %s", response.Error.Error())
	}

	log.Printf("[MCP Client] Deregistered agent: %s", c.agentID)
	return nil
}

// SendMessage sends a directed message
func (c *Client) SendMessage(msgType, to string, payload map[string]interface{}) error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	if msgType == "" {
		return errors.New("message type cannot be empty")
	}
	if to == "" {
		return errors.New("recipient cannot be empty")
	}

	msg := Message{
		ID:        c.generateMessageID(),
		Type:      msgType,
		From:      c.agentID,
		To:        to,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	params := map[string]interface{}{"message": msg}

	ctx, cancel := context.WithTimeout(context.Background(), c.writeTimeout)
	defer cancel()

	_, err := c.sendJSONRPCRequest(ctx, "agent.send", params)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// Broadcast sends a message to all agents
func (c *Client) Broadcast(msgType string, payload map[string]interface{}) error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	if msgType == "" {
		return errors.New("message type cannot be empty")
	}

	msg := Message{
		ID:        c.generateMessageID(),
		Type:      msgType,
		From:      c.agentID,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	params := map[string]interface{}{"message": msg}

	ctx, cancel := context.WithTimeout(context.Background(), c.writeTimeout)
	defer cancel()

	_, err := c.sendJSONRPCRequest(ctx, "agent.broadcast", params)
	if err != nil {
		return fmt.Errorf("failed to broadcast message: %w", err)
	}

	return nil
}

// SendLog sends a log message
func (c *Client) SendLog(level, message string) error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	if level == "" {
		return errors.New("log level cannot be empty")
	}
	if message == "" {
		return errors.New("log message cannot be empty")
	}

	params := map[string]interface{}{
		"agent_id":  c.agentID,
		"level":     level,
		"message":   message,
		"timestamp": time.Now().Unix(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.writeTimeout)
	defer cancel()

	return c.sendJSONRPCNotification(ctx, "agent.log", params)
}

// messageListener reads messages from WebSocket
func (c *Client) messageListener() {
	defer func() {
		log.Println("[MCP Client] Message listener stopped")
	}()

	for {
		select {
		case <-c.stopChan:
			return
		default:
		}

		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil {
			return
		}

		conn.SetReadDeadline(time.Now().Add(2 * c.heartbeatInterval))
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Printf("[MCP Client] WebSocket error: %v", err)
			}

			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()

			select {
			case c.reconnectChan <- struct{}{}:
			default:
			}
			return
		}

		var msg JSONRPCMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[MCP Client] Failed to unmarshal message: %v", err)
			continue
		}

		// Handle responses to pending requests
		if msg.ID != nil && msg.Method == "" {
			c.requestMu.Lock()
			if ch, ok := c.pending[msg.ID]; ok {
				select {
				case ch <- &msg:
					delete(c.pending, msg.ID)
				default:
				}
			}
			c.requestMu.Unlock()
			continue
		}

		// Handle incoming messages
		if msg.Method == "agent.message" {
			var params struct {
				Message Message `json:"message"`
			}
			if err := json.Unmarshal(msg.Params, &params); err == nil {
				select {
				case c.msgChan <- params.Message:
					c.mu.RLock()
					handler := c.messageHandler
					c.mu.RUnlock()
					if handler != nil {
						go handler(params.Message)
					}
				default:
					log.Println("[MCP Client] Message channel full, dropping message")
				}
			}
		}
	}
}

// heartbeat sends periodic ping messages
func (c *Client) heartbeat() {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			connected := c.connected
			c.mu.RUnlock()

			if !connected || conn == nil {
				continue
			}

			deadline := time.Now().Add(c.writeTimeout)
			if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				log.Printf("[MCP Client] Ping failed: %v", err)

				c.mu.Lock()
				c.connected = false
				c.mu.Unlock()

				select {
				case c.reconnectChan <- struct{}{}:
				default:
				}
			}
		}
	}
}

// reconnectMonitor handles automatic reconnection with exponential backoff
func (c *Client) reconnectMonitor() {
	for {
		select {
		case <-c.stopChan:
			return
		case <-c.reconnectChan:
			if c.IsConnected() {
				continue
			}

			log.Println("[MCP Client] Attempting to reconnect...")

			for attempt := 1; attempt <= c.reconnectMaxAttempts; attempt++ {
				select {
				case <-c.stopChan:
					return
				default:
				}

				delay := c.calculateBackoff(attempt)
				log.Printf("[MCP Client] Reconnect attempt %d/%d in %v", attempt, c.reconnectMaxAttempts, delay)
				time.Sleep(delay)

				c.stopChan = make(chan struct{})

				if err := c.connectInternal(); err == nil {
					log.Println("[MCP Client] Successfully reconnected")
					go c.messageListener()
					go c.heartbeat()
					go c.reconnectMonitor()
					return
				}

				log.Printf("[MCP Client] Reconnect attempt %d failed", attempt)
			}

			log.Printf("[MCP Client] Failed to reconnect after %d attempts", c.reconnectMaxAttempts)
		}
	}
}

// connectInternal establishes connection without starting goroutines
func (c *Client) connectInternal() error {
	headers := http.Header{}
	headers.Set("X-Agent-ID", c.agentID)

	conn, resp, err := c.dialer.Dial(c.url, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	return nil
}

// calculateBackoff computes delay for reconnection
func (c *Client) calculateBackoff(attempt int) time.Duration {
	backoff := float64(c.reconnectBaseDelay) * math.Pow(2, float64(attempt-1))
	if backoff > float64(c.reconnectMaxDelay) {
		backoff = float64(c.reconnectMaxDelay)
	}

	jitter := backoff * 0.25
	backoff = backoff - jitter + (float64(time.Now().UnixNano()%1000) / 1000.0 * jitter * 2)

	return time.Duration(backoff)
}

// sendJSONRPCRequest sends a request and waits for response
func (c *Client) sendJSONRPCRequest(ctx context.Context, method string, params interface{}) (*JSONRPCMessage, error) {
	c.requestMu.Lock()
	c.requestID++
	id := c.requestID
	c.requestMu.Unlock()

	paramsData, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	msg := JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  paramsData,
	}

	responseChan := make(chan *JSONRPCMessage, 1)
	c.requestMu.Lock()
	c.pending[id] = responseChan
	c.requestMu.Unlock()

	defer func() {
		c.requestMu.Lock()
		delete(c.pending, id)
		c.requestMu.Unlock()
	}()

	if err := c.writeJSONRPCMessage(ctx, msg); err != nil {
		return nil, err
	}

	select {
	case response := <-responseChan:
		if response == nil {
			return nil, errors.New("connection closed while waiting for response")
		}
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// sendJSONRPCNotification sends a notification
func (c *Client) sendJSONRPCNotification(ctx context.Context, method string, params interface{}) error {
	paramsData, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}

	msg := JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsData,
	}

	return c.writeJSONRPCMessage(ctx, msg)
}

// writeJSONRPCMessage writes a message to WebSocket
func (c *Client) writeJSONRPCMessage(ctx context.Context, msg JSONRPCMessage) error {
	c.mu.RLock()
	conn := c.conn
	connected := c.connected
	c.mu.RUnlock()

	if !connected || conn == nil {
		return errors.New("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.conn != nil {
			done <- c.conn.WriteMessage(websocket.TextMessage, data)
		} else {
			done <- errors.New("connection closed")
		}
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// generateMessageID creates a unique message ID
func (c *Client) generateMessageID() string {
	return fmt.Sprintf("%s-%d", c.agentID, time.Now().UnixNano())
}

// GetMessageChannel returns the message channel
func (c *Client) GetMessageChannel() <-chan Message {
	return c.msgChan
}

// GetAgentID returns the agent ID
func (c *Client) GetAgentID() string {
	return c.agentID
}

// GetURL returns the server URL
func (c *Client) GetURL() string {
	return c.url
}

// ClientStatus represents the connection status
type ClientStatus struct {
	Connected     bool      `json:"connected"`
	URL           string    `json:"url"`
	AgentID       string    `json:"agent_id"`
	LastHeartbeat time.Time `json:"last_heartbeat,omitempty"`
	Reconnects    int       `json:"reconnects"`
	Enabled       bool      `json:"enabled"`
}

// GetStatus returns the current status
func (c *Client) GetStatus() ClientStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return ClientStatus{
		Connected:  c.connected,
		URL:        c.url,
		AgentID:    c.agentID,
		Reconnects: c.reconnectMaxAttempts,
		Enabled:    true,
	}
}
