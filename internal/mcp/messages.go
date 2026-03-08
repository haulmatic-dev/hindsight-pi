package mcp

import (
	"encoding/json"
	"fmt"
	"time"
)

// Message types for MCP communication
const (
	MsgTypeRegistration   = "registration"
	MsgTypeDeregistration = "deregistration"
	MsgTypeTaskStart      = "task_start"
	MsgTypeTaskComplete   = "task_complete"
	MsgTypeLog            = "log"
	MsgTypeHeartbeat      = "heartbeat"
	MsgTypeError          = "error"
	MsgTypeCommand        = "command"
	MsgTypeFileReserved   = "file_reserved"
	MsgTypeFileReleased   = "file_released"
)

// JSON-RPC 2.0 constants
const (
	JSONRPCVersion = "2.0"
)

// Message represents an MCP protocol message.
// It follows JSON-RPC 2.0 format for compatibility with standard tooling.
type Message struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	From      string                 `json:"from"`
	To        string                 `json:"to,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// RegistrationMetadata contains information about agent registration
type RegistrationMetadata struct {
	AgentID   string            `json:"agent_id"`
	BeadID    string            `json:"bead_id"`
	Type      string            `json:"type"`
	Project   string            `json:"project"`
	Repo      string            `json:"repo,omitempty"`
	StartTime time.Time         `json:"start_time"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error implements the error interface
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC Error %d: %s", e.Code, e.Message)
}

// JSONRPCRequest represents a JSON-RPC 2.0 request message
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response message
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// DeregistrationMetadata contains information about agent deregistration
type DeregistrationMetadata struct {
	AgentID   string    `json:"agent_id"`
	BeadID    string    `json:"bead_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason,omitempty"`
}

// TaskStartPayload contains information about task start
type TaskStartPayload struct {
	BeadID    string            `json:"bead_id"`
	StartTime time.Time         `json:"start_time"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// TaskCompletePayload contains information about task completion
type TaskCompletePayload struct {
	BeadID   string        `json:"bead_id"`
	Status   string        `json:"status"` // success, failure, partial
	Duration time.Duration `json:"duration"`
	ExitCode int           `json:"exit_code"`
	Message  string        `json:"message,omitempty"`
}

// LogPayload contains log information
type LogPayload struct {
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source,omitempty"` // source file/line
	BeadID    string    `json:"bead_id,omitempty"`
}

// HeartbeatPayload contains heartbeat information
type HeartbeatPayload struct {
	AgentID   string    `json:"agent_id"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"` // healthy, busy, idle
}

// ErrorPayload contains error information
type ErrorPayload struct {
	Code      int       `json:"code"`
	Message   string    `json:"message"`
	Details   string    `json:"details,omitempty"`
	BeadID    string    `json:"bead_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// CommandPayload contains command information
type CommandPayload struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Options map[string]string `json:"options,omitempty"`
	BeadID  string            `json:"bead_id,omitempty"`
	Timeout time.Duration     `json:"timeout,omitempty"`
}

// FileReservedPayload contains file reservation information
type FileReservedPayload struct {
	BeadID   string    `json:"bead_id"`
	FilePath string    `json:"file_path"`
	Reserved time.Time `json:"reserved_at"`
	Expires  time.Time `json:"expires_at,omitempty"`
}

// FileReleasedPayload contains file release information
type FileReleasedPayload struct {
	BeadID    string    `json:"bead_id"`
	FilePath  string    `json:"file_path"`
	Released  time.Time `json:"released_at"`
	Committed bool      `json:"committed"` // true if changes were committed
}

// NewMessage creates a new MCP message with the given type, sender, and payload.
func NewMessage(msgType, from string, payload map[string]interface{}) *Message {
	return &Message{
		ID:        generateMessageID(),
		Type:      msgType,
		From:      from,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// SetRecipient sets the recipient of the message.
func (m *Message) SetRecipient(to string) {
	m.To = to
}

// ToJSON serializes the message to JSON bytes.
func (m *Message) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// ParseMessage parses JSON bytes into a Message.
func ParseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}
	return &msg, nil
}

// ToJSONRPCRequest wraps the message in a JSON-RPC request format.
func (m *Message) ToJSONRPCRequest() *JSONRPCRequest {
	return &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      m.ID,
		Method:  m.Type,
		Params:  m,
	}
}

// ToJSONRPCResponse creates a JSON-RPC response from this message.
func (m *Message) ToJSONRPCResponse(result interface{}) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      m.ID,
		Result:  result,
	}
}

// ToJSONRPCError creates a JSON-RPC error response from this message.
func (m *Message) ToJSONRPCError(code int, message string, data interface{}) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      m.ID,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// ToJSON serializes the JSON-RPC request to JSON bytes.
func (r *JSONRPCRequest) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// ToJSON serializes the JSON-RPC response to JSON bytes.
func (r *JSONRPCResponse) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// ParseJSONRPCRequest parses JSON bytes into a JSONRPCRequest.
func ParseJSONRPCRequest(data []byte) (*JSONRPCRequest, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse JSON-RPC request: %w", err)
	}
	if req.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("unsupported JSON-RPC version: %s", req.JSONRPC)
	}
	return &req, nil
}

// ParseJSONRPCResponse parses JSON bytes into a JSONRPCResponse.
func ParseJSONRPCResponse(data []byte) (*JSONRPCResponse, error) {
	var resp JSONRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON-RPC response: %w", err)
	}
	if resp.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("unsupported JSON-RPC version: %s", resp.JSONRPC)
	}
	return &resp, nil
}

// ExtractMessage extracts the MCP Message from a JSON-RPC request params.
func (r *JSONRPCRequest) ExtractMessage() (*Message, error) {
	paramsBytes, err := json.Marshal(r.Params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}
	return ParseMessage(paramsBytes)
}

// IsSuccess returns true if the response indicates success (no error).
func (r *JSONRPCResponse) IsSuccess() bool {
	return r.Error == nil
}

// IsError returns true if the response indicates an error.
func (r *JSONRPCResponse) IsError() bool {
	return r.Error != nil
}

// Standard RPC Error Codes
const (
	// ParseErrorInvalidJSON is sent when the JSON payload is invalid
	ParseErrorInvalidJSON = -32700
	// InvalidRequestError is sent when the request object is invalid
	InvalidRequestError = -32600
	// MethodNotFoundError is sent when the method does not exist
	MethodNotFoundError = -32601
	// InvalidParamsError is sent when the parameters are invalid
	InvalidParamsError = -32602
	// InternalError is sent for internal errors
	InternalError = -32603
	// ServerErrorReserved is the start of the reserved server error range
	ServerErrorReserved = -32000
)

// generateMessageID creates a unique message ID.
func generateMessageID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixMicro())
}

// NewRegistrationMessage creates a registration message with proper metadata.
func NewRegistrationMessage(from string, metadata map[string]interface{}) *Message {
	return NewMessage(MsgTypeRegistration, from, metadata)
}

// NewDeregistrationMessage creates a deregistration message.
func NewDeregistrationMessage(from string, metadata DeregistrationMetadata) *Message {
	payload := map[string]interface{}{
		"agent_id": metadata.AgentID,
	}

	if metadata.BeadID != "" {
		payload["bead_id"] = metadata.BeadID
	}
	if metadata.Reason != "" {
		payload["reason"] = metadata.Reason
	}

	return NewMessage(MsgTypeDeregistration, from, payload)
}

// NewTaskStartMessage creates a task start message.
func NewTaskStartMessage(from string, payload TaskStartPayload) *Message {
	p := map[string]interface{}{
		"bead_id":    payload.BeadID,
		"start_time": payload.StartTime,
	}

	if payload.Metadata != nil && len(payload.Metadata) > 0 {
		p["metadata"] = payload.Metadata
	}

	return NewMessage(MsgTypeTaskStart, from, p)
}

// NewTaskCompleteMessage creates a task completion message.
func NewTaskCompleteMessage(from string, payload TaskCompletePayload) *Message {
	p := map[string]interface{}{
		"bead_id":   payload.BeadID,
		"status":    payload.Status,
		"duration":  payload.Duration,
		"exit_code": payload.ExitCode,
	}

	if payload.Message != "" {
		p["message"] = payload.Message
	}

	return NewMessage(MsgTypeTaskComplete, from, p)
}

// NewLogMessage creates a log message.
func NewLogMessage(from string, payload LogPayload) *Message {
	p := map[string]interface{}{
		"level":   payload.Level,
		"message": payload.Message,
	}

	if payload.Source != "" {
		p["source"] = payload.Source
	}
	if payload.BeadID != "" {
		p["bead_id"] = payload.BeadID
	}

	return NewMessage(MsgTypeLog, from, p)
}

// NewHeartbeatMessage creates a heartbeat message.
func NewHeartbeatMessage(from string, payload HeartbeatPayload) *Message {
	p := map[string]interface{}{
		"agent_id":  payload.AgentID,
		"timestamp": payload.Timestamp,
		"status":    payload.Status,
	}

	return NewMessage(MsgTypeHeartbeat, from, p)
}

// NewErrorMessage creates an error message.
func NewErrorMessage(from string, payload ErrorPayload) *Message {
	p := map[string]interface{}{
		"code":      payload.Code,
		"message":   payload.Message,
		"timestamp": payload.Timestamp,
	}

	if payload.Details != "" {
		p["details"] = payload.Details
	}
	if payload.BeadID != "" {
		p["bead_id"] = payload.BeadID
	}

	return NewMessage(MsgTypeError, from, p)
}

// NewCommandMessage creates a command message.
func NewCommandMessage(from string, payload CommandPayload) *Message {
	p := map[string]interface{}{
		"command": payload.Command,
	}

	if len(payload.Args) > 0 {
		p["args"] = payload.Args
	}
	if len(payload.Options) > 0 {
		p["options"] = payload.Options
	}
	if payload.BeadID != "" {
		p["bead_id"] = payload.BeadID
	}
	if payload.Timeout > 0 {
		p["timeout"] = payload.Timeout
	}

	return NewMessage(MsgTypeCommand, from, p)
}

// NewFileReservedMessage creates a file reserved message.
func NewFileReservedMessage(from string, payload FileReservedPayload) *Message {
	p := map[string]interface{}{
		"bead_id":     payload.BeadID,
		"file_path":   payload.FilePath,
		"reserved_at": payload.Reserved,
	}

	if !payload.Expires.IsZero() {
		p["expires_at"] = payload.Expires
	}

	return NewMessage(MsgTypeFileReserved, from, p)
}

// NewFileReleasedMessage creates a file released message.
func NewFileReleasedMessage(from string, payload FileReleasedPayload) *Message {
	p := map[string]interface{}{
		"bead_id":   payload.BeadID,
		"file_path": payload.FilePath,
		"released":  payload.Released,
		"committed": payload.Committed,
	}

	return NewMessage(MsgTypeFileReleased, from, p)
}

// IsValidStatus checks if the given status is valid for task completion.
func IsValidStatus(status string) bool {
	switch status {
	case "success", "failure", "partial":
		return true
	default:
		return false
	}
}

// IsValidLogLevel checks if the given log level is valid.
func IsValidLogLevel(level string) bool {
	switch level {
	case "debug", "info", "warn", "warning", "error", "fatal":
		return true
	default:
		return false
	}
}
