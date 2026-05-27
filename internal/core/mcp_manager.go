package core

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/internal/services"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// MCPManager manages the Model Context Protocol (MCP) client and message handling.
type MCPManager struct {
	mu                 sync.RWMutex
	client             *mcp.HTTPClient
	reservationMgr     *mcp.ReservationManager
	config             *MCPConfig
	activeReservations map[string]*mcp.Reservation
	messageHandler     func(mcp.Message)
	overseer           *Overseer
}

// NewMCPManager creates a new MCP manager.
func NewMCPManager(overseer *Overseer) *MCPManager {
	return &MCPManager{overseer: overseer}
}

// Initialize sets up the MCP client if enabled.
func (m *MCPManager) Initialize(config *MCPConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !config.Enabled {
		return nil
	}

	m.config = config
	m.activeReservations = make(map[string]*mcp.Reservation)

	ttl := config.FileReservationTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	m.reservationMgr = mcp.NewReservationManager()

	agentID := fmt.Sprintf("hydra-overseer-%d", time.Now().Unix())
	httpURL := config.URL
	if httpURL == "" {
		httpURL = fmt.Sprintf("http://127.0.0.1:%d", config.Port)
	}
	if len(httpURL) > 5 && httpURL[:5] == "ws://" {
		httpURL = "http://" + httpURL[5:]
	}
	if len(httpURL) > 3 && httpURL[len(httpURL)-3:] == "/ws" {
		httpURL = httpURL[:len(httpURL)-3]
	}

	var err error
	m.client, err = mcp.NewHTTPClient(httpURL, agentID)
	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	m.client.SetMessageHandler(m.handleMessage)

	if err := m.StartListener(); err != nil {
		return fmt.Errorf("failed to start MCP listener: %w", err)
	}

	log.Printf("[MCP] MCP client initialized (URL: %s, Agent: %s)", httpURL, agentID)
	return nil
}

// StartListener connects to the MCP server and registers the agent.
func (m *MCPManager) StartListener() error {
	if m.client == nil {
		return fmt.Errorf("MCP client not initialized")
	}

	if err := m.client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	metadata := mcp.RegistrationMetadata{
		AgentID:   "hydra-overseer",
		Type:      "orchestrator",
		StartTime: time.Now(),
		Metadata: map[string]string{
			"role":    "overseer",
			"version": "1.0.0",
		},
	}

	if err := m.client.Register(metadata); err != nil {
		log.Printf("[MCP] Debug: registration failed: %v", err)
	} else {
		log.Printf("[MCP] Successfully registered with MCP server")
	}

	return nil
}

// Stop disconnects from the MCP server.
func (m *MCPManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return nil
	}

	if m.client.IsConnected() {
		if err := m.client.Deregister(mcp.RegistrationMetadata{}); err != nil {
			log.Printf("[MCP] Failed to deregister: %v", err)
		}
	}

	if err := m.client.Disconnect(); err != nil {
		log.Printf("[MCP] Error during disconnect: %v", err)
		return err
	}

	log.Printf("[MCP] Disconnected from MCP server")
	return nil
}

// handleMessage processes incoming MCP messages.
func (m *MCPManager) handleMessage(msg mcp.Message) {
	log.Printf("[MCP] Received message: type=%s, from=%s", msg.Type, msg.From)

	switch msg.Type {
	case mcp.MsgTypeTaskComplete:
		m.handleTaskComplete(&msg)
	case mcp.MsgTypeTaskStart:
		m.handleTaskStart(&msg)
	case mcp.MsgTypeHeartbeat:
		log.Printf("[MCP] Heartbeat from %s", msg.From)
	case mcp.MsgTypeLog:
		m.handleLog(&msg)
	case mcp.MsgTypeFileReserved:
		m.handleFileReserved(&msg)
	case mcp.MsgTypeFileReleased:
		m.handleFileReleased(&msg)
	default:
		log.Printf("[MCP] Unknown message type: %s", msg.Type)
	}
}

// HandleMessage processes an MCP message (public API).
func (m *MCPManager) HandleMessage(msg *mcp.Message) error {
	if msg == nil {
		return fmt.Errorf("nil message")
	}
	m.handleMessage(*msg)
	return nil
}

func (m *MCPManager) handleTaskComplete(msg *mcp.Message) {
	beadID, ok := msg.Payload["bead_id"].(string)
	if !ok || beadID == "" {
		log.Printf("[MCP] Task complete message missing bead_id")
		return
	}

	status, _ := msg.Payload["status"].(string)
	exitCode := 0
	if code, ok := msg.Payload["exit_code"].(float64); ok {
		exitCode = int(code)
	}

	log.Printf("[MCP] Task complete: bead=%s, status=%s, exit_code=%d", beadID, status, exitCode)

	if m.overseer.intelligenceHub.BatchRetention() != nil {
		m.submitExecutionResult(msg)
	} else {
		log.Printf("[MCP] Warning: batch retention service not available, skipping Hindsight retention")
	}

	go m.overseer.triggerImmediateSchedule()
}

func (m *MCPManager) submitExecutionResult(msg *mcp.Message) {
	beadIDStr, ok := msg.Payload["bead_id"].(string)
	if !ok || beadIDStr == "" {
		return
	}

	beadID := types.BeadID(beadIDStr)
	status, _ := msg.Payload["status"].(string)
	success := status == "success"

	exitCode := 0
	if code, ok := msg.Payload["exit_code"].(float64); ok {
		exitCode = int(code)
	}

	duration := 0 * time.Second
	if dur, ok := msg.Payload["duration"].(float64); ok {
		duration = time.Duration(dur * float64(time.Second))
	}

	attempts := 1
	if att, ok := msg.Payload["attempts"].(float64); ok {
		attempts = int(att)
	}

	agentType, _ := msg.Payload["agent_type"].(string)
	output, _ := msg.Payload["output"].(string)
	errorStr, _ := msg.Payload["error"].(string)
	workerID, _ := msg.Payload["agent_id"].(string)

	bead := types.Bead{
		ID:    beadID,
		Title: getStringFromPayload(msg.Payload, "bead_title"),
		Type:  getStringFromPayload(msg.Payload, "bead_type"),
	}

	execResult := services.ExecutionResult{
		BeadID:    beadID,
		Success:   success,
		ExitCode:  exitCode,
		Output:    output,
		Error:     errorStr,
		Duration:  duration,
		Attempts:  attempts,
		AgentType: agentType,
		Timestamp: time.Now(),
	}

	m.overseer.intelligenceHub.SubmitExecutionResult(bead, execResult, workerID)
}

func (m *MCPManager) handleTaskStart(msg *mcp.Message) {
	beadID, ok := msg.Payload["bead_id"].(string)
	if !ok || beadID == "" {
		return
	}
	log.Printf("[MCP] Task started: bead=%s, agent=%s", beadID, msg.From)
}

func (m *MCPManager) handleLog(msg *mcp.Message) {
	level, _ := msg.Payload["level"].(string)
	message, _ := msg.Payload["message"].(string)
	beadID, _ := msg.Payload["bead_id"].(string)
	log.Printf("[MCP Log] [%s] [bead=%s] %s", level, beadID, message)
}

func (m *MCPManager) handleFileReserved(msg *mcp.Message) {
	beadID, _ := msg.Payload["bead_id"].(string)
	filePath, _ := msg.Payload["file_path"].(string)
	log.Printf("[MCP] File reserved: bead=%s, file=%s", beadID, filePath)
}

func (m *MCPManager) handleFileReleased(msg *mcp.Message) {
	beadID, _ := msg.Payload["bead_id"].(string)
	filePath, _ := msg.Payload["file_path"].(string)
	committed, _ := msg.Payload["committed"].(bool)
	log.Printf("[MCP] File released: bead=%s, file=%s, committed=%v", beadID, filePath, committed)
}

// GetActiveReservations returns all active file reservations.
func (m *MCPManager) GetActiveReservations() []*mcp.Reservation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.reservationMgr == nil {
		return nil
	}

	allRes := m.reservationMgr.GetAllReservations()
	var active []*mcp.Reservation
	for _, res := range allRes {
		if !res.IsExpired() {
			active = append(active, res)
		}
	}
	return active
}

// ForceReleaseReservation forcibly releases a reservation by ID.
func (m *MCPManager) ForceReleaseReservation(reservationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.reservationMgr == nil {
		return fmt.Errorf("reservation manager not initialized")
	}

	m.reservationMgr.RemoveReservation(reservationID)
	log.Printf("[MCP] Force released reservation: %s", reservationID)
	return nil
}

// Broadcast sends a broadcast message to all connected workers.
func (m *MCPManager) Broadcast(msgType string, payload map[string]interface{}) error {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("MCP client not initialized")
	}
	if !client.IsConnected() {
		return fmt.Errorf("MCP client not connected")
	}

	if err := client.Broadcast(msgType, payload); err != nil {
		return fmt.Errorf("failed to broadcast message: %w", err)
	}

	log.Printf("[MCP] Broadcast message sent: type=%s", msgType)
	return nil
}

// SendLog sends a log message to the MCP server.
func (m *MCPManager) SendLog(level, message string) error {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("MCP client not available")
	}

	return client.SendLog("", level, message)
}

// Status returns the current MCP connection status.
func (m *MCPManager) Status() mcp.ClientStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.client == nil {
		return mcp.ClientStatus{
			Enabled:   m.overseer.config.MCP.Enabled,
			Connected: false,
		}
	}
	return m.client.GetStatus()
}

// IsConnected returns whether the MCP client is connected.
func (m *MCPManager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.client == nil {
		return false
	}
	return m.client.IsConnected()
}

// SendWorkerCompletion sends a task completion broadcast for a worker.
func (m *MCPManager) SendWorkerCompletion(worker *types.Worker, success bool, exitCode int, duration time.Duration) {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return
	}

	status := "success"
	if !success {
		if m.overseer.retryManager.GetRetryCount(worker.ID) >= m.overseer.config.Overseer.DefaultRetries {
			status = "failure"
		} else {
			status = "partial"
		}
	}

	payload := map[string]interface{}{
		"bead_id":    worker.ID,
		"status":     status,
		"exit_code":  exitCode,
		"duration":   duration.String(),
		"agent_type": string(worker.AgentType),
	}

	if err := client.Broadcast(mcp.MsgTypeTaskComplete, payload); err != nil {
		log.Printf("[MCP] Debug: task completion broadcast failed: %v", err)
	} else {
		log.Printf("[MCP] Task completion broadcast: bead=%s, status=%s", worker.ID, status)
	}
}

// getStringFromPayload safely extracts a string value from a map
func getStringFromPayload(payload map[string]interface{}, key string) string {
	if val, ok := payload[key].(string); ok {
		return val
	}
	return ""
}
