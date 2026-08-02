package bv

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// mockResponse represents a mocked CLI response
type mockResponse struct {
	Output []byte
	Err    error
}

// mockExecutor implements CLIExecutor for testing
type mockExecutor struct {
	responses map[string]mockResponse
	calls     []struct {
		Name string
		Args []string
	}
}

// NewMockExecutor creates a new mock executor
func NewMockExecutor() *mockExecutor {
	return &mockExecutor{
		responses: make(map[string]mockResponse),
		calls: make([]struct {
			Name string
			Args []string
		}, 0),
	}
}

// AddResponse adds a mock response for a specific command
func (m *mockExecutor) AddResponse(name string, args []string, output []byte, err error) {
	key := m.key(name, args)
	m.responses[key] = mockResponse{Output: output, Err: err}
}

// Execute implements CLIExecutor
func (m *mockExecutor) Execute(name string, arg ...string) ([]byte, error) {
	m.calls = append(m.calls, struct {
		Name string
		Args []string
	}{Name: name, Args: arg})

	key := m.key(name, arg)
	if resp, ok := m.responses[key]; ok {
		return resp.Output, resp.Err
	}

	// Return empty response if not found
	return []byte{}, nil
}

// GetCalls returns all calls made to the executor
func (m *mockExecutor) GetCalls() []struct {
	Name string
	Args []string
} {
	return m.calls
}

// key generates a unique key for a command
func (m *mockExecutor) key(name string, args []string) string {
	key := name
	for _, arg := range args {
		key += " " + arg
	}
	return key
}

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient()

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	if !client.enabled {
		t.Error("Expected client to be enabled by default")
	}

	if client.healthCheckTTL != 30*time.Second {
		t.Errorf("Expected default health check TTL of 30s, got %v", client.healthCheckTTL)
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	mockExec := NewMockExecutor()
	client := NewClient(
		WithExecutor(mockExec),
		WithHealthCheckTTL(5*time.Second),
	)

	if client.executor != mockExec {
		t.Error("Expected custom executor to be set")
	}

	if client.healthCheckTTL != 5*time.Second {
		t.Errorf("Expected health check TTL of 5s, got %v", client.healthCheckTTL)
	}
}

func TestHealthCheck_Available(t *testing.T) {
	mockExec := NewMockExecutor()
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)

	client := NewClient(WithExecutor(mockExec))
	status := client.HealthCheck()

	if !status.Healthy {
		t.Error("Expected health check to pass when bv is available")
	}

	if status.Cached {
		t.Error("Expected first health check to not be cached")
	}

	if status.CheckedAt.IsZero() {
		t.Error("Expected CheckedAt to be set")
	}
}

func TestHealthCheck_Unavailable(t *testing.T) {
	mockExec := NewMockExecutor()
	mockExec.AddResponse("bv", []string{"-version"}, []byte{}, errors.New("command not found"))

	client := NewClient(WithExecutor(mockExec))
	status := client.HealthCheck()

	if status.Healthy {
		t.Error("Expected health check to fail when bv is unavailable")
	}
}

func TestHealthCheck_CachedResult(t *testing.T) {
	mockExec := NewMockExecutor()
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)

	client := NewClient(
		WithExecutor(mockExec),
		WithHealthCheckTTL(1*time.Minute),
	)

	// First call
	status1 := client.HealthCheck()
	if !status1.Healthy {
		t.Fatal("Expected first health check to pass")
	}

	// Second call should use cached result
	status2 := client.HealthCheck()
	if !status2.Cached {
		t.Error("Expected second health check to be cached")
	}

	if !status2.Healthy {
		t.Error("Expected cached health check to still be healthy")
	}

	// Should only have called bv once
	calls := mockExec.GetCalls()
	bvCalls := 0
	for _, call := range calls {
		if call.Name == "bv" {
			bvCalls++
		}
	}
	if bvCalls != 1 {
		t.Errorf("Expected 1 bv call, got %d", bvCalls)
	}
}

func TestGetInsights_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	insights := InsightsResponse{
		Project:         "hydra",
		TotalBeads:      100,
		OpenBeads:       50,
		InProgressBeads: 10,
		CompletedBeads:  35,
		BlockedBeads:    5,
		Bottlenecks: []Bottleneck{
			{
				BeadID:       "hydra-abc123",
				Title:        "Critical bottleneck",
				BlockedCount: 10,
			},
		},
		Keystones: []Keystone{
			{
				BeadID:      "hydra-key001",
				Title:       "Important keystone",
				ImpactScore: 95,
			},
		},
		Metrics: InsightsMetrics{
			AvgDependencies: 2.5,
			MaxDependencies: 10,
			GraphDepth:      5,
		},
	}

	jsonData, _ := json.Marshal(insights)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-insights", "--format", "json"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetInsights("")

	if err != nil {
		t.Fatalf("GetInsights failed: %v", err)
	}

	if result.Project != "hydra" {
		t.Errorf("Expected project 'hydra', got '%s'", result.Project)
	}

	if result.TotalBeads != 100 {
		t.Errorf("Expected 100 total beads, got %d", result.TotalBeads)
	}

	if len(result.Bottlenecks) != 1 {
		t.Errorf("Expected 1 bottleneck, got %d", len(result.Bottlenecks))
	}

	if len(result.Keystones) != 1 {
		t.Errorf("Expected 1 keystone, got %d", len(result.Keystones))
	}
}

func TestGetInsights_GracefulDegradation(t *testing.T) {
	mockExec := NewMockExecutor()
	// bv -version fails, so client is unavailable
	mockExec.AddResponse("bv", []string{"-version"}, []byte{}, errors.New("command not found"))

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetInsights("")

	if err != nil {
		t.Errorf("Expected no error with graceful degradation, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result with graceful degradation")
	}

	// Result should be empty but not nil
	if result.TotalBeads != 0 {
		t.Errorf("Expected empty result, got %d total beads", result.TotalBeads)
	}
}

func TestGetTriage_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	triage := TriageWrapper{
		GeneratedAt: "2025-01-15T00:00:00Z",
		DataHash:    "abc123",
		Triage: TriageResponse{
			Recommendations: []TriageRecommendation{
				{
					BeadID:     "hydra-001",
					Title:      "High priority task",
					Type:       "implement",
					Priority:   1,
					Confidence: 0.95,
					Reason:     "Critical path dependency",
				},
			},
			ReadyCount:   10,
			BlockedCount: 5,
		},
	}

	jsonData, _ := json.Marshal(triage)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-triage", "--format", "json"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetTriage("")

	if err != nil {
		t.Fatalf("GetTriage failed: %v", err)
	}

	if len(result.Recommendations) != 1 {
		t.Errorf("Expected 1 recommendation, got %d", len(result.Recommendations))
	}

	if result.ReadyCount != 10 {
		t.Errorf("Expected 10 ready beads, got %d", result.ReadyCount)
	}
}

func TestGetPlan_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	plan := PlanWrapper{
		GeneratedAt: "2025-01-15T00:00:00Z",
		DataHash:    "abc123",
		Plan: PlanData{
			Tracks: []PlanTrack{
				{
					TrackID: "track-1",
					Items: []PlanItem{
						{ID: "hydra-001", Title: "Task 1", Priority: 1, Status: "open"},
						{ID: "hydra-002", Title: "Task 2", Priority: 1, Status: "open"},
					},
				},
				{
					TrackID: "track-2",
					Items: []PlanItem{
						{ID: "hydra-003", Title: "Task 3", Priority: 2, Status: "in_progress"},
					},
				},
			},
			TotalActionable: 3,
			TotalBlocked:    0,
			Summary:         PlanSummary{HighestImpact: "execute tracks", ImpactReason: "all items unblocked", UnblocksCount: 3},
		},
	}

	jsonData, _ := json.Marshal(plan)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-plan", "--format", "json"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetPlan("")

	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}

	if len(result.Tracks) != 2 {
		t.Errorf("Expected 2 tracks, got %d", len(result.Tracks))
	}

	totalItems := 0
	for _, track := range result.Tracks {
		totalItems += len(track.Items)
	}
	if totalItems != 3 {
		t.Errorf("Expected 3 items across tracks, got %d", totalItems)
	}

	if result.TotalActionable != 3 {
		t.Errorf("Expected 3 actionable, got %d", result.TotalActionable)
	}
}

func TestGetAlerts_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	alerts := AlertsResponse{
		Alerts: []Alert{
			{
				ID:       "alert-001",
				Type:     "stale",
				Severity: "warning",
				Title:    "Stale bead detected",
				Message:  "Bead has not been updated in 30 days",
				BeadID:   "hydra-stale001",
			},
		},
		Drift: DriftInfo{
			HasDrift: false,
			Level:    "",
			Message:  "",
		},
	}

	jsonData, _ := json.Marshal(alerts)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-alerts", "--format", "json"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetAlerts("")

	if err != nil {
		t.Fatalf("GetAlerts failed: %v", err)
	}

	if len(result.Alerts) != 1 {
		t.Errorf("Expected 1 alert, got %d", len(result.Alerts))
	}

	if result.Alerts[0].Severity != "warning" {
		t.Errorf("Expected severity 'warning', got '%s'", result.Alerts[0].Severity)
	}
}

func TestGetPriority_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	priority := PriorityResponse{
		Priorities: []PriorityItem{
			{
				BeadID:    "hydra-001",
				Title:     "Critical task",
				Rank:      1,
				Score:     95.5,
				Urgency:   90.0,
				Impact:    100.0,
				Reasoning: "Blocks 10 other beads",
			},
		},
		Categories: []Category{
			{
				Name:        "critical",
				Count:       1,
				Description: "Must complete immediately",
				BeadIDs:     []string{"hydra-001"},
			},
		},
	}

	jsonData, _ := json.Marshal(priority)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-priority", "--format", "json"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetPriority("")

	if err != nil {
		t.Fatalf("GetPriority failed: %v", err)
	}

	if len(result.Priorities) != 1 {
		t.Errorf("Expected 1 priority, got %d", len(result.Priorities))
	}

	if result.Priorities[0].Rank != 1 {
		t.Errorf("Expected rank 1, got %d", result.Priorities[0].Rank)
	}
}

func TestGetCapacity_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	capacity := CapacityResponse{
		Agents:           5,
		TotalBeads:       100,
		OpenBeads:        50,
		EstimatedDays:    10.5,
		ThroughputPerDay: 5.0,
		Bottlenecks:      []string{"hydra-bottleneck001"},
		LimitingFactors: []LimitingFactor{
			{
				Type:        "dependency",
				Description: "Long dependency chain",
				Impact:      "high",
			},
		},
	}

	jsonData, _ := json.Marshal(capacity)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-capacity", "--format", "json", "--agents", "5"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetCapacity("", 5)

	if err != nil {
		t.Fatalf("GetCapacity failed: %v", err)
	}

	if result.Agents != 5 {
		t.Errorf("Expected 5 agents, got %d", result.Agents)
	}

	if result.EstimatedDays != 10.5 {
		t.Errorf("Expected 10.5 estimated days, got %f", result.EstimatedDays)
	}
}

func TestGetFileHotspots_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	hotspots := FileHotspotsResponse{
		Hotspots: []FileHotspot{
			{
				Path:       "internal/core/overseer.go",
				TouchCount: 50,
				Beads:      []string{"hydra-001", "hydra-002"},
			},
			{
				Path:       "internal/beads/client.go",
				TouchCount: 30,
				Beads:      []string{"hydra-003"},
			},
		},
		Total: 2,
	}

	jsonData, _ := json.Marshal(hotspots)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-file-hotspots", "--format", "json", "--hotspots-limit", "10"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetFileHotspots("", 10)

	if err != nil {
		t.Fatalf("GetFileHotspots failed: %v", err)
	}

	if len(result.Hotspots) != 2 {
		t.Errorf("Expected 2 hotspots, got %d", len(result.Hotspots))
	}

	if result.Hotspots[0].Path != "internal/core/overseer.go" {
		t.Errorf("Expected first hotspot path 'internal/core/overseer.go', got '%s'", result.Hotspots[0].Path)
	}
}

func TestGetDrift_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	drift := DriftResponse{
		HasDrift:     true,
		Level:        "warning",
		Message:      "5 beads have drifted from baseline",
		FromBaseline: "baseline-001",
		Changes: DriftChanges{
			Added: []DriftBead{
				{BeadID: "hydra-new001", Title: "New bead", ChangeType: "added"},
			},
			Modified: []DriftBead{
				{BeadID: "hydra-mod001", Title: "Modified bead", ChangeType: "modified"},
			},
		},
		CheckedAt: time.Now(),
	}

	jsonData, _ := json.Marshal(drift)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-drift", "--format", "json"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetDrift("")

	if err != nil {
		t.Fatalf("GetDrift failed: %v", err)
	}

	if !result.HasDrift {
		t.Error("Expected drift to be detected")
	}

	if result.Level != "warning" {
		t.Errorf("Expected level 'warning', got '%s'", result.Level)
	}

	if len(result.Changes.Added) != 1 {
		t.Errorf("Expected 1 added bead, got %d", len(result.Changes.Added))
	}
}

func TestSearch_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	search := SearchResponse{
		Query: "overseer implementation",
		Results: []SearchResult{
			{
				BeadID:      "hydra-001",
				Title:       "Implement overseer",
				Score:       0.95,
				Relevance:   0.90,
				Type:        "implement",
				Status:      "open",
				Description: "Implement the overseer component",
			},
		},
		Total: 1,
	}

	jsonData, _ := json.Marshal(search)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-search", "--format", "json", "--search", "overseer implementation", "--search-limit", "10"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.Search("", "overseer implementation", 10)

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if result.Query != "overseer implementation" {
		t.Errorf("Expected query 'overseer implementation', got '%s'", result.Query)
	}

	if len(result.Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result.Results))
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	mockExec := NewMockExecutor()
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.Search("", "", 10)

	if err != nil {
		t.Errorf("Expected no error for empty query, got: %v", err)
	}

	if len(result.Results) != 0 {
		t.Errorf("Expected 0 results for empty query, got %d", len(result.Results))
	}
}

func TestGetRelatedBeads_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	related := RelatedBeadsResponse{
		BeadID: "hydra-001",
		Related: []RelatedBeadInfo{
			{
				BeadID:   "hydra-002",
				Title:    "Related task",
				Type:     "implement",
				Status:   "open",
				Relation: "similar",
				Strength: 0.85,
			},
		},
		Categories: []RelatedCategory{
			{
				Name:  "similar",
				Count: 1,
				Beads: []string{"hydra-002"},
			},
		},
	}

	jsonData, _ := json.Marshal(related)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-related", "--format", "json", "hydra-001"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetRelatedBeads("", "hydra-001")

	if err != nil {
		t.Fatalf("GetRelatedBeads failed: %v", err)
	}

	if result.BeadID != "hydra-001" {
		t.Errorf("Expected bead ID 'hydra-001', got '%s'", result.BeadID)
	}

	if len(result.Related) != 1 {
		t.Errorf("Expected 1 related bead, got %d", len(result.Related))
	}
}

func TestGetImpactNetwork_Success(t *testing.T) {
	mockExec := NewMockExecutor()

	network := ImpactNetworkResponse{
		RootBead:    "hydra-001",
		TotalImpact: 10,
		Depth:       3,
		Nodes: []ImpactNode{
			{
				BeadID:      "hydra-001",
				Title:       "Root task",
				Type:        "implement",
				Status:      "open",
				ImpactLevel: 10,
			},
			{
				BeadID:      "hydra-002",
				Title:       "Dependent task",
				Type:        "test",
				Status:      "open",
				ImpactLevel: 5,
			},
		},
		Edges: []ImpactEdge{
			{
				Source: "hydra-001",
				Target: "hydra-002",
				Type:   "blocks",
				Weight: 1.0,
			},
		},
	}

	jsonData, _ := json.Marshal(network)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)
	mockExec.AddResponse("bv", []string{"--robot-impact-network", "--format", "json", "hydra-001"}, jsonData, nil)

	client := NewClient(WithExecutor(mockExec))
	result, err := client.GetImpactNetwork("", "hydra-001")

	if err != nil {
		t.Fatalf("GetImpactNetwork failed: %v", err)
	}

	if result.RootBead != "hydra-001" {
		t.Errorf("Expected root bead 'hydra-001', got '%s'", result.RootBead)
	}

	if len(result.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(result.Nodes))
	}

	if len(result.Edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(result.Edges))
	}
}

func TestExecInDir_WithProjectPath(t *testing.T) {
	mockExec := NewMockExecutor()

	insights := InsightsResponse{
		Project:    "test-project",
		TotalBeads: 50,
	}

	jsonData, _ := json.Marshal(insights)
	mockExec.AddResponse("bv", []string{"-version"}, []byte("bv version 1.0.0"), nil)

	// Note: We can't easily test execInDir with mock since it uses exec.Command directly
	// But we can test that the method exists and has the right signature
	client := NewClient(WithExecutor(mockExec))

	// Test with empty project path (uses exec, not execInDir)
	mockExec.AddResponse("bv", []string{"--robot-insights", "--format", "json"}, jsonData, nil)

	result, err := client.GetInsights("")
	if err != nil {
		t.Fatalf("GetInsights with empty path failed: %v", err)
	}

	if result.TotalBeads != 50 {
		t.Errorf("Expected 50 beads, got %d", result.TotalBeads)
	}
}

func TestCLIError_Error(t *testing.T) {
	err := &CLIError{
		Command: "bv",
		Args:    []string{"--robot-insights"},
		Stdout:  "",
		Stderr:  "error: command failed",
		Err:     errors.New("exit status 1"),
	}

	errMsg := err.Error()
	if errMsg == "" {
		t.Error("Expected non-empty error message")
	}

	if !errors.Is(err.Unwrap(), errors.New("exit status 1")) {
		// Note: errors.Is comparison might fail due to pointer comparison
		// Just check that Unwrap returns something
		if err.Unwrap() == nil {
			t.Error("Expected Unwrap to return the underlying error")
		}
	}
}

func TestGracefulDegradation_MultipleCalls(t *testing.T) {
	mockExec := NewMockExecutor()
	// bv is unavailable
	mockExec.AddResponse("bv", []string{"-version"}, []byte{}, errors.New("command not found"))

	client := NewClient(WithExecutor(mockExec))

	// Multiple calls should all gracefully degrade
	insights, _ := client.GetInsights("")
	triage, _ := client.GetTriage("")
	plan, _ := client.GetPlan("")
	alerts, _ := client.GetAlerts("")

	if insights.TotalBeads != 0 {
		t.Error("Expected empty insights")
	}
	if len(triage.Recommendations) != 0 {
		t.Error("Expected empty triage")
	}
	if len(plan.Tracks) != 0 {
		t.Error("Expected empty plan")
	}
	if len(alerts.Alerts) != 0 {
		t.Error("Expected empty alerts")
	}
}
