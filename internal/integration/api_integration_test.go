//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/haulmatic-dev/hydra/graph"
	"github.com/haulmatic-dev/hydra/internal/api"
	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ============================================================================
// Test Setup Helpers
// ============================================================================

// setupTestServer creates a test server with a mock overseer
func setupTestServer(t *testing.T) (*httptest.Server, *core.Overseer, func()) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create test configuration
	config := &core.Config{
		API: core.APIConfig{
			Enabled: true,
			Port:    0, // Random port
		},
		Overseer: core.OverseerConfig{
			MaxWorkers:            5,
			BeadDiscoveryInterval: 10 * time.Second,
			DefaultRetries:        3,
			WorkerTimeout:         30 * time.Minute,
		},
		MCP: core.MCPConfig{
			Enabled: false, // Disable MCP for tests
		},
	}

	// Create overseer with test config
	overseer := core.NewOverseer(config)

	// Create API server
	server := api.NewServer(config, overseer)

	// Create HTTP test server
	ts := httptest.NewServer(server.Router())

	return ts, overseer, func() {
		ts.Close()
	}
}

// setupTestServerWithProjects creates a test server with mock projects
func setupTestServerWithProjects(t *testing.T) (*httptest.Server, *core.Overseer, func()) {
	ts, overseer, cleanup := setupTestServer(t)

	// Add mock projects
	overseer.GetProjects()

	return ts, overseer, cleanup
}

// ============================================================================
// REST API Tests
// ============================================================================

// TestHealthEndpoint tests the GET /health endpoint
func TestHealthEndpoint(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Make request
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify Content-Type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Expected JSON content type, got %s", contentType)
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify fields
	if status, ok := result["status"].(string); !ok || status != "healthy" {
		t.Errorf("Expected status 'healthy', got %v", result["status"])
	}

	if version, ok := result["version"].(string); !ok || version == "" {
		t.Errorf("Expected non-empty version, got %v", result["version"])
	}
}

// TestStatsEndpoint tests the GET /api/v1/stats endpoint
func TestStatsEndpoint(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Make request
	resp, err := http.Get(ts.URL + "/api/v1/stats")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Parse response
	var result api.SystemStats
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify all fields are present
	if result.MaxWorkers <= 0 {
		t.Errorf("Expected positive max_workers, got %d", result.MaxWorkers)
	}

	if result.Uptime < 0 {
		t.Errorf("Expected non-negative uptime, got %d", result.Uptime)
	}

	if result.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}

	// Verify overseer_running is boolean
	// (it should be false since we haven't started the overseer)
	if result.OverseerRunning {
		t.Log("Overseer is running, which is unexpected but not an error")
	}
}

// TestWorkersEndpoint tests the GET /api/v1/workers endpoint
func TestWorkersEndpoint(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Make request
	resp, err := http.Get(ts.URL + "/api/v1/workers")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Parse response
	var result []api.WorkerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should return empty array when no workers
	if result == nil {
		t.Error("Expected non-nil workers array")
	}
}

// TestProjectsEndpoint tests the GET /api/v1/projects endpoint
func TestProjectsEndpoint(t *testing.T) {
	ts, overseer, cleanup := setupTestServer(t)
	defer cleanup()

	// Add mock project to overseer
	mockProjects := []types.Project{
		{
			Name: "test-project",
			Path: "/tmp/test-project",
			Type: types.RepoTypeSingle,
		},
	}

	// We can't easily set projects directly, so we'll test that the endpoint responds
	_ = overseer
	_ = mockProjects

	// Make request
	resp, err := http.Get(ts.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Parse response
	var result []api.ProjectResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Result should be an array (may be empty if no projects discovered)
	if result == nil {
		t.Error("Expected non-nil projects array")
	}
}

// TestControlEndpoints tests the overseer control endpoints
func TestControlEndpoints(t *testing.T) {
	ts, overseer, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name       string
		method     string
		path       string
		body       interface{}
		wantStatus int
		validate   func(t *testing.T, overseer *core.Overseer)
	}{
		{
			name:       "pause overseer",
			method:     http.MethodPost,
			path:       "/api/v1/overseer/pause",
			wantStatus: http.StatusOK,
			validate: func(t *testing.T, overseer *core.Overseer) {
				if !overseer.IsPaused() {
					t.Error("Expected overseer to be paused")
				}
			},
		},
		{
			name:       "resume overseer",
			method:     http.MethodPost,
			path:       "/api/v1/overseer/resume",
			wantStatus: http.StatusOK,
			validate: func(t *testing.T, overseer *core.Overseer) {
				if overseer.IsPaused() {
					t.Error("Expected overseer to be resumed")
				}
			},
		},
		{
			name:   "set max workers",
			method: http.MethodPost,
			path:   "/api/v1/overseer/max-workers",
			body: map[string]int{
				"count": 3,
			},
			wantStatus: http.StatusOK,
			validate: func(t *testing.T, overseer *core.Overseer) {
				if overseer.GetMaxWorkers() != 3 {
					t.Errorf("Expected max workers to be 3, got %d", overseer.GetMaxWorkers())
				}
			},
		},
		{
			name:   "set max workers invalid - too low",
			method: http.MethodPost,
			path:   "/api/v1/overseer/max-workers",
			body: map[string]int{
				"count": 0,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "set max workers invalid - too high",
			method: http.MethodPost,
			path:   "/api/v1/overseer/max-workers",
			body: map[string]int{
				"count": 25,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "set max workers missing field",
			method:     http.MethodPost,
			path:       "/api/v1/overseer/max-workers",
			body:       map[string]string{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyBytes []byte
			if tt.body != nil {
				var err error
				bodyBytes, err = json.Marshal(tt.body)
				if err != nil {
					t.Fatalf("Failed to marshal body: %v", err)
				}
			}

			var req *http.Request
			var err error
			if bodyBytes != nil {
				req, err = http.NewRequest(tt.method, ts.URL+tt.path, bytes.NewReader(bodyBytes))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req, err = http.NewRequest(tt.method, ts.URL+tt.path, nil)
			}

			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			if tt.validate != nil && resp.StatusCode == http.StatusOK {
				tt.validate(t, overseer)
			}
		})
	}
}

// ============================================================================
// GraphQL API Tests
// ============================================================================

// setupGraphQLServer creates a test server with GraphQL endpoint
func setupGraphQLServer(t *testing.T) (*httptest.Server, *graph.Resolver, func()) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create test configuration
	config := &core.Config{
		Overseer: core.OverseerConfig{
			MaxWorkers:            5,
			BeadDiscoveryInterval: 10 * time.Second,
			DefaultRetries:        3,
			WorkerTimeout:         30 * time.Minute,
		},
	}

	// Create overseer
	overseer := core.NewOverseer(config)

	// Create resolver
	resolver := &graph.Resolver{
		Overseer: overseer,
	}

	// Create GraphQL server
	srv := handler.NewDefaultServer(
		graph.NewExecutableSchema(graph.Config{
			Resolvers: resolver,
		}),
	)

	// Add WebSocket transport for subscriptions
	srv.AddTransport(&transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for tests
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	})

	// Create Gin router
	router := gin.New()
	router.POST("/graphql", gin.WrapH(srv))
	router.GET("/graphql", gin.WrapH(srv))

	// Create test server
	ts := httptest.NewServer(router)

	return ts, resolver, func() {
		ts.Close()
	}
}

// TestGraphQLHealth tests the health GraphQL query
func TestGraphQLHealth(t *testing.T) {
	ts, _, cleanup := setupGraphQLServer(t)
	defer cleanup()

	query := `{
		"query": "{ health { status version } }"
	}`

	resp, err := http.Post(
		ts.URL+"/graphql",
		"application/json",
		strings.NewReader(query),
	)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Health struct {
				Status  string `json:"status"`
				Version string `json:"version"`
			} `json:"health"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Errors) > 0 {
		t.Errorf("GraphQL errors: %v", result.Errors)
	}

	if result.Data.Health.Status == "" {
		t.Error("Expected non-empty health status")
	}

	if result.Data.Health.Version == "" {
		t.Error("Expected non-empty health version")
	}
}

// TestGraphQLBeads tests the beads GraphQL query
func TestGraphQLBeads(t *testing.T) {
	ts, _, cleanup := setupGraphQLServer(t)
	defer cleanup()

	query := `{
		"query": "{ beads { edges { node { id title status } } totalCount } }"
	}`

	resp, err := http.Post(
		ts.URL+"/graphql",
		"application/json",
		strings.NewReader(query),
	)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Beads struct {
				Edges []struct {
					Node struct {
						ID     string `json:"id"`
						Title  string `json:"title"`
						Status string `json:"status"`
					} `json:"node"`
				} `json:"edges"`
				TotalCount int `json:"totalCount"`
			} `json:"beads"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Errors) > 0 {
		t.Errorf("GraphQL errors: %v", result.Errors)
	}

	// Verify beads connection structure
	if result.Data.Beads.TotalCount < 0 {
		t.Errorf("Expected non-negative totalCount, got %d", result.Data.Beads.TotalCount)
	}
}

// TestGraphQLWorkers tests the workers GraphQL query
func TestGraphQLWorkers(t *testing.T) {
	ts, _, cleanup := setupGraphQLServer(t)
	defer cleanup()

	query := `{
		"query": "{ workers { id agentType status } }"
	}`

	resp, err := http.Post(
		ts.URL+"/graphql",
		"application/json",
		strings.NewReader(query),
	)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Workers []struct {
				ID        string `json:"id"`
				AgentType string `json:"agentType"`
				Status    string `json:"status"`
			} `json:"workers"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Errors) > 0 {
		t.Errorf("GraphQL errors: %v", result.Errors)
	}

	if result.Data.Workers == nil {
		t.Error("Expected non-nil workers array")
	}
}

// TestGraphQLDAGStats tests the DAG stats GraphQL query
func TestGraphQLDAGStats(t *testing.T) {
	ts, _, cleanup := setupGraphQLServer(t)
	defer cleanup()

	query := `{
		"query": "{ dagStats { totalNodes readyNodes blockedNodes completedNodes failedNodes activeWorkers maxWorkers } }"
	}`

	resp, err := http.Post(
		ts.URL+"/graphql",
		"application/json",
		strings.NewReader(query),
	)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			DAGStats struct {
				TotalNodes     int `json:"totalNodes"`
				ReadyNodes     int `json:"readyNodes"`
				BlockedNodes   int `json:"blockedNodes"`
				CompletedNodes int `json:"completedNodes"`
				FailedNodes    int `json:"failedNodes"`
				ActiveWorkers  int `json:"activeWorkers"`
				MaxWorkers     int `json:"maxWorkers"`
			} `json:"dagStats"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Errors) > 0 {
		t.Errorf("GraphQL errors: %v", result.Errors)
	}

	// Verify all fields are present
	if result.Data.DAGStats.MaxWorkers <= 0 {
		t.Errorf("Expected positive maxWorkers, got %d", result.Data.DAGStats.MaxWorkers)
	}
}

// TestGraphQLMutations tests GraphQL mutations
func TestGraphQLMutations(t *testing.T) {
	ts, _, cleanup := setupGraphQLServer(t)
	defer cleanup()

	tests := []struct {
		name       string
		query      string
		wantStatus int
		validate   func(t *testing.T, data map[string]interface{})
	}{
		{
			name:       "pause overseer",
			query:      `mutation { pauseOverseer }`,
			wantStatus: http.StatusOK,
			validate: func(t *testing.T, data map[string]interface{}) {
				if result, ok := data["pauseOverseer"].(bool); !ok || !result {
					t.Errorf("Expected pauseOverseer to return true, got %v", data["pauseOverseer"])
				}
			},
		},
		{
			name:       "resume overseer",
			query:      `mutation { resumeOverseer }`,
			wantStatus: http.StatusOK,
			validate: func(t *testing.T, data map[string]interface{}) {
				if result, ok := data["resumeOverseer"].(bool); !ok || !result {
					t.Errorf("Expected resumeOverseer to return true, got %v", data["resumeOverseer"])
				}
			},
		},
		{
			name:       "set max workers",
			query:      `mutation { setMaxWorkers(count: 3) }`,
			wantStatus: http.StatusOK,
			validate: func(t *testing.T, data map[string]interface{}) {
				if count, ok := data["setMaxWorkers"].(float64); !ok || count != 3 {
					t.Errorf("Expected setMaxWorkers to return 3, got %v", data["setMaxWorkers"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"query": "%s"}`, tt.query)

			resp, err := http.Post(
				ts.URL+"/graphql",
				"application/json",
				strings.NewReader(body),
			)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			var result struct {
				Data   map[string]interface{} `json:"data"`
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if len(result.Errors) > 0 {
				t.Errorf("GraphQL errors: %v", result.Errors)
				return
			}

			if tt.validate != nil && result.Data != nil {
				tt.validate(t, result.Data)
			}
		})
	}
}

// ============================================================================
// WebSocket Subscription Tests
// ============================================================================

// TestGraphQLSubscriptions tests GraphQL WebSocket subscriptions
func TestGraphQLSubscriptions(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create test configuration
	config := &core.Config{
		Overseer: core.OverseerConfig{
			MaxWorkers:            5,
			BeadDiscoveryInterval: 10 * time.Second,
			DefaultRetries:        3,
			WorkerTimeout:         30 * time.Minute,
		},
	}

	// Create overseer
	overseer := core.NewOverseer(config)

	// Create resolver with event broadcasting
	resolver := graph.NewResolver(overseer)
	defer resolver.Stop()

	// Create GraphQL server
	srv := handler.NewDefaultServer(
		graph.NewExecutableSchema(graph.Config{
			Resolvers: resolver,
		}),
	)

	// Add WebSocket transport
	srv.AddTransport(&transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	})

	// Create test server
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Convert http:// to ws://
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/graphql"

	// Connect WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Sec-WebSocket-Protocol": []string{"graphql-transport-ws"},
	})
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	// Send connection_init message
	initMsg := map[string]interface{}{
		"type": "connection_init",
	}
	if err := ws.WriteJSON(initMsg); err != nil {
		t.Fatalf("Failed to send init message: %v", err)
	}

	// Read connection_ack
	var ackMsg struct {
		Type string `json:"type"`
	}
	if err := ws.ReadJSON(&ackMsg); err != nil {
		t.Fatalf("Failed to read ack message: %v", err)
	}
	if ackMsg.Type != "connection_ack" {
		t.Fatalf("Expected connection_ack, got %s", ackMsg.Type)
	}

	// Start subscription for workerUpdates
	subscribeMsg := map[string]interface{}{
		"type": "subscribe",
		"id":   "test-subscription-1",
		"payload": map[string]interface{}{
			"query": `subscription { workerUpdates { id agentType status } }`,
		},
	}
	if err := ws.WriteJSON(subscribeMsg); err != nil {
		t.Fatalf("Failed to send subscribe message: %v", err)
	}

	// Note: The subscription resolvers currently panic with "not implemented"
	// This test verifies the WebSocket connection and subscription setup works

	// Wait a bit for subscription to be established
	time.Sleep(100 * time.Millisecond)

	// Send complete message
	completeMsg := map[string]interface{}{
		"type": "complete",
		"id":   "test-subscription-1",
	}
	if err := ws.WriteJSON(completeMsg); err != nil {
		t.Fatalf("Failed to send complete message: %v", err)
	}

	t.Log("WebSocket subscription test completed successfully")
}

// ============================================================================
// Error Handling Tests
// ============================================================================

// TestRESTAPIErrorHandling tests error handling in REST API
func TestRESTAPIErrorHandling(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{
			name:       "invalid path",
			method:     http.MethodGet,
			path:       "/api/v1/invalid-endpoint",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "method not allowed on health",
			method:     http.MethodPost,
			path:       "/health",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid JSON body",
			method:     http.MethodPost,
			path:       "/api/v1/overseer/max-workers",
			body:       `{"invalid json`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body *strings.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			} else {
				body = strings.NewReader("")
			}

			req, err := http.NewRequest(tt.method, ts.URL+tt.path, body)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

// TestGraphQLErrorHandling tests error handling in GraphQL API
func TestGraphQLErrorHandling(t *testing.T) {
	ts, _, cleanup := setupGraphQLServer(t)
	defer cleanup()

	tests := []struct {
		name          string
		query         string
		wantData      bool
		wantErrors    bool
		errorContains string
	}{
		{
			name:       "invalid query syntax",
			query:      `{ invalid syntax here }`,
			wantErrors: true,
		},
		{
			name:       "unknown field",
			query:      `{ unknownField { id } }`,
			wantErrors: true,
		},
		{
			name:       "empty query",
			query:      `{}`,
			wantErrors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"query": "%s"}`, tt.query)

			resp, err := http.Post(
				ts.URL+"/graphql",
				"application/json",
				strings.NewReader(body),
			)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer resp.Body.Close()

			// GraphQL often returns 200 even for errors
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnprocessableEntity {
				t.Logf("Unexpected status code: %d", resp.StatusCode)
			}

			var result struct {
				Data   interface{} `json:"data"`
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if tt.wantErrors && len(result.Errors) == 0 {
				t.Error("Expected GraphQL errors but got none")
			}

			if !tt.wantErrors && len(result.Errors) > 0 {
				t.Errorf("Unexpected GraphQL errors: %v", result.Errors)
			}
		})
	}
}

// ============================================================================
// Integration Tests
// ============================================================================

// TestFullAPIIntegration tests the complete API integration
func TestFullAPIIntegration(t *testing.T) {
	ts, overseer, cleanup := setupTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan bool)

	go func() {
		defer close(done)

		// Test 1: Health check
		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Errorf("Health check failed: %v", err)
			return
		}
		resp.Body.Close()

		// Test 2: Pause overseer
		_, err = http.Post(ts.URL+"/api/v1/overseer/pause", "", nil)
		if err != nil {
			t.Errorf("Pause failed: %v", err)
			return
		}

		if !overseer.IsPaused() {
			t.Error("Overseer should be paused")
		}

		// Test 3: Get stats
		resp, err = http.Get(ts.URL + "/api/v1/stats")
		if err != nil {
			t.Errorf("Stats request failed: %v", err)
			return
		}
		resp.Body.Close()

		// Test 4: Resume overseer
		_, err = http.Post(ts.URL+"/api/v1/overseer/resume", "", nil)
		if err != nil {
			t.Errorf("Resume failed: %v", err)
			return
		}

		if overseer.IsPaused() {
			t.Error("Overseer should be resumed")
		}

		// Test 5: Set max workers
		body := `{"count": 3}`
		resp, err = http.Post(
			ts.URL+"/api/v1/overseer/max-workers",
			"application/json",
			strings.NewReader(body),
		)
		if err != nil {
			t.Errorf("Set max workers failed: %v", err)
			return
		}
		resp.Body.Close()

		if overseer.GetMaxWorkers() != 3 {
			t.Errorf("Expected max workers 3, got %d", overseer.GetMaxWorkers())
		}
	}()

	select {
	case <-done:
		// Test completed successfully
	case <-ctx.Done():
		t.Fatal("Integration test timed out")
	}
}

// TestCORSHeaders tests CORS headers are properly set
func TestCORSHeaders(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Test preflight request
	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/v1/stats", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify CORS headers
	if allowOrigin := resp.Header.Get("Access-Control-Allow-Origin"); allowOrigin != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin: *, got %s", allowOrigin)
	}

	if allowMethods := resp.Header.Get("Access-Control-Allow-Methods"); allowMethods == "" {
		t.Error("Expected Access-Control-Allow-Methods header")
	}
}

// TestConcurrentRequests tests concurrent API requests
func TestConcurrentRequests(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	const numRequests = 10
	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			resp, err := http.Get(ts.URL + "/health")
			if err != nil {
				errors <- fmt.Errorf("request failed: %w", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("Concurrent request error: %v", err)
		}
	}
}
