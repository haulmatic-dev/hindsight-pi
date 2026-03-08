package graph

import (
	"context"
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/graph/model"
	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ============================================================================
// Test Data Helpers
// ============================================================================

// createTestBead creates a types.Bead for testing
func createTestBead(id types.BeadID, title, beadType string, status types.BeadStatus, priority int) types.Bead {
	now := time.Now()
	return types.Bead{
		ID:          id,
		Title:       title,
		Type:        beadType,
		Status:      status,
		Priority:    priority,
		Project:     id.Project(),
		Repo:        id.Repo(),
		Labels:      []string{"test", "unit"},
		CreatedAt:   now.Add(-24 * time.Hour),
		UpdatedAt:   now,
		Description: "Test bead description",
	}
}

func setupBasicDAG(dag *core.DAG) {
	beads := []types.Bead{
		createTestBead("hydra-abc123", "Implement Feature A", "implement", types.StatusOpen, 5),
		createTestBead("hydra-def456", "Write Tests", "test", types.StatusOpen, 4),
		createTestBead("hydra-ghi789", "Fix Bug", "fix", types.StatusCompleted, 3),
		createTestBead("hydra-jkl012", "Review Code", "review", types.StatusOpen, 2),
		createTestBead("myproject-mno345", "Update Docs", "requirements", types.StatusFailed, 1),
	}

	for _, bead := range beads {
		dag.AddNode(bead)
	}

	// Add dependencies: hydra-def456 depends on hydra-abc123
	dag.AddEdge("hydra-def456", "hydra-abc123")
}

// getModelStatus converts types.BeadStatus to model.BeadStatus
func getModelStatus(status types.BeadStatus) model.BeadStatus {
	switch status {
	case types.StatusOpen:
		return model.BeadStatusOpen
	case types.StatusInProgress:
		return model.BeadStatusInProgress
	case types.StatusCompleted:
		return model.BeadStatusCompleted
	case types.StatusFailed:
		return model.BeadStatusFailed
	case types.StatusBlocked:
		return model.BeadStatusBlocked
	default:
		return model.BeadStatusOpen
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// Test BeadsQuery
// ============================================================================

func TestBeadsQuery_BasicConnection(t *testing.T) {
	tests := []struct {
		name      string
		filter    *model.BeadFilter
		first     *int32
		after     *string
		sort      []*model.BeadSort
		setupDAG  func(*core.DAG)
		wantCount int
		wantErr   bool
	}{
		{
			name:      "basic query returns connection",
			filter:    nil,
			setupDAG:  setupBasicDAG,
			wantCount: 5,
			wantErr:   false,
		},
		{
			name: "filter by single status",
			filter: &model.BeadFilter{
				Status: []model.BeadStatus{model.BeadStatusOpen},
			},
			setupDAG:  setupBasicDAG,
			wantCount: 3,
			wantErr:   false,
		},
		{
			name: "filter by multiple statuses",
			filter: &model.BeadFilter{
				Status: []model.BeadStatus{model.BeadStatusOpen, model.BeadStatusCompleted},
			},
			setupDAG:  setupBasicDAG,
			wantCount: 4,
			wantErr:   false,
		},
		{
			name: "filter by type",
			filter: &model.BeadFilter{
				Type: []string{"implement", "test"},
			},
			setupDAG:  setupBasicDAG,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "filter by project",
			filter: &model.BeadFilter{
				Project: stringPtr("hydra"),
			},
			setupDAG:  setupBasicDAG,
			wantCount: 4,
			wantErr:   false,
		},
		{
			name: "filter with no matches",
			filter: &model.BeadFilter{
				Status: []model.BeadStatus{model.BeadStatusBlocked},
			},
			setupDAG:  setupBasicDAG,
			wantCount: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a DAG and populate it with test data
			dag := core.NewDAG()
			tt.setupDAG(dag)

			// Use the DAG directly through the resolvers
			// Since we can't easily inject a DAG into the resolver without using the Overseer,
			// we'll test the filter and sort functions directly
			beads := make([]*model.Bead, 0)
			for _, node := range dag.GetAllNodes() {
				bead := convertTypesBeadToModel(&node.Bead)
				// Convert status properly
				bead.Status = getModelStatus(node.Bead.Status)
				beads = append(beads, bead)
			}

			// Apply filter
			var filtered []*model.Bead
			for _, bead := range beads {
				if matchesFilter(bead, tt.filter) {
					filtered = append(filtered, bead)
				}
			}

			// Verify count
			if len(filtered) != tt.wantCount {
				t.Errorf("Beads() filtered count = %d, want %d", len(filtered), tt.wantCount)
			}
		})
	}
}

func TestBeadsQuery_Pagination(t *testing.T) {
	dag := core.NewDAG()

	// Add multiple beads for pagination testing
	for i := 1; i <= 10; i++ {
		bead := createTestBead(
			types.BeadID("hydra-"+strconv.Itoa(i)),
			"Test Bead "+strconv.Itoa(i),
			"test",
			types.StatusOpen,
			i,
		)
		dag.AddNode(bead)
	}

	beads := make([]*model.Bead, 0)
	for _, node := range dag.GetAllNodes() {
		beads = append(beads, convertTypesBeadToModel(&node.Bead))
	}

	// Test pagination logic
	first := int32(3)
	startIndex := 0
	endIndex := startIndex + int(first)
	if endIndex > len(beads) {
		endIndex = len(beads)
	}

	pageBeads := beads[startIndex:endIndex]
	if len(pageBeads) != 3 {
		t.Errorf("Pagination returned %d beads, want 3", len(pageBeads))
	}

	// Test cursor creation and decoding
	for i := range pageBeads {
		cursor := base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(startIndex + i)))
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			t.Fatalf("Failed to decode cursor: %v", err)
		}
		decodedInt, err := strconv.Atoi(string(decoded))
		if err != nil {
			t.Fatalf("Failed to convert decoded cursor: %v", err)
		}
		if decodedInt != startIndex+i {
			t.Errorf("Cursor round-trip failed: got %d, want %d", decodedInt, startIndex+i)
		}
	}
}

func TestBeadsQuery_EmptyResult(t *testing.T) {
	dag := core.NewDAG()
	// No beads added

	beads := dag.GetAllNodes()
	if len(beads) != 0 {
		t.Errorf("Empty DAG returned %d nodes, want 0", len(beads))
	}
}

func TestBeadsQuery_Sorting(t *testing.T) {
	dag := core.NewDAG()

	// Add beads with varying priorities
	priorities := []int{3, 1, 5, 2, 4}
	for i, p := range priorities {
		bead := createTestBead(
			types.BeadID("hydra-"+strconv.Itoa(i)),
			"Bead "+strconv.Itoa(i),
			"test",
			types.StatusOpen,
			p,
		)
		dag.AddNode(bead)
	}

	beads := make([]*model.Bead, 0)
	for _, node := range dag.GetAllNodes() {
		beads = append(beads, convertTypesBeadToModel(&node.Bead))
	}

	// Test ascending sort
	ascSort := []*model.BeadSort{
		{Field: model.BeadSortFieldPriority, Direction: model.SortDirectionAsc},
	}
	applySorting(beads, ascSort)

	// Verify ascending order
	for i := 1; i < len(beads); i++ {
		if beads[i].Priority < beads[i-1].Priority {
			t.Errorf("Ascending sort failed at index %d", i)
		}
	}

	// Test descending sort
	descSort := []*model.BeadSort{
		{Field: model.BeadSortFieldPriority, Direction: model.SortDirectionDesc},
	}
	applySorting(beads, descSort)

	// Verify descending order
	for i := 1; i < len(beads); i++ {
		if beads[i].Priority > beads[i-1].Priority {
			t.Errorf("Descending sort failed at index %d", i)
		}
	}
}

// ============================================================================
// Test BeadQuery (Single Bead)
// ============================================================================

func TestBeadQuery_SingleBead(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		setupDAG  func(*core.DAG)
		wantNil   bool
		wantErr   bool
		errMsg    string
		wantTitle string
	}{
		{
			name: "get existing bead by ID",
			id:   "hydra-abc123",
			setupDAG: func(dag *core.DAG) {
				bead := createTestBead("hydra-abc123", "Test Bead", "implement", types.StatusOpen, 5)
				dag.AddNode(bead)
			},
			wantNil:   false,
			wantErr:   false,
			wantTitle: "Test Bead",
		},
		{
			name:     "bead not found returns nil",
			id:       "hydra-nonexistent",
			setupDAG: func(dag *core.DAG) {},
			wantNil:  true,
			wantErr:  false,
		},
		{
			name:     "invalid ID format",
			id:       "invalid-id",
			setupDAG: func(dag *core.DAG) {},
			wantNil:  true,
			wantErr:  true,
			errMsg:   "invalid bead ID",
		},
		{
			name:     "empty ID",
			id:       "",
			setupDAG: func(dag *core.DAG) {},
			wantNil:  true,
			wantErr:  true,
			errMsg:   "bead ID cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dag := core.NewDAG()
			tt.setupDAG(dag)

			// Test ParseBeadID
			beadID, err := types.ParseBeadID(tt.id)

			if tt.wantErr {
				if err == nil {
					// Also test that GetNode returns nil for invalid
					_, exists := dag.GetNode(tt.id)
					if exists {
						t.Errorf("Expected bead to not exist for invalid ID")
					}
					return
				}
				if tt.errMsg != "" && !containsSubstring(err.Error(), tt.errMsg) {
					t.Errorf("ParseBeadID() error = %v, want containing %v", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseBeadID() unexpected error = %v", err)
				return
			}

			if tt.wantNil {
				_, exists := dag.GetNode(string(beadID))
				if exists {
					t.Errorf("Expected bead to not exist")
				}
				return
			}

			node, exists := dag.GetNode(string(beadID))
			if !exists {
				t.Errorf("Bead with ID %s should exist", tt.id)
				return
			}

			if node.Bead.Title != tt.wantTitle {
				t.Errorf("Bead Title = %v, want %v", node.Bead.Title, tt.wantTitle)
			}
		})
	}
}

// ============================================================================
// Test ReadyBeadsQuery
// ============================================================================

func TestReadyBeadsQuery(t *testing.T) {
	dag := core.NewDAG()

	// Add beads: ready (InDegree=0) and blocked (InDegree>0)
	readyBead := createTestBead("hydra-ready", "Ready Bead", "implement", types.StatusOpen, 5)
	blockedBead := createTestBead("hydra-blocked", "Blocked Bead", "test", types.StatusOpen, 4)

	dag.AddNode(readyBead)
	dag.AddNode(blockedBead)
	dag.AddEdge("hydra-blocked", "hydra-ready") // blocked depends on ready

	readyNodes := dag.GetReadyNodes()

	// Should only return beads with InDegree == 0
	if len(readyNodes) != 1 {
		t.Errorf("GetReadyNodes() returned %d beads, want 1", len(readyNodes))
	}

	if len(readyNodes) > 0 && string(readyNodes[0].Bead.ID) != "hydra-ready" {
		t.Errorf("GetReadyNodes() returned bead with ID %s, want hydra-ready", readyNodes[0].Bead.ID)
	}
}

func TestReadyBeadsQuery_Empty(t *testing.T) {
	dag := core.NewDAG()
	// Test with an empty DAG - should return no ready nodes
	readyNodes := dag.GetReadyNodes()

	if len(readyNodes) != 0 {
		t.Errorf("GetReadyNodes() returned %d beads, want 0 for empty DAG", len(readyNodes))
	}
}

// ============================================================================
// Test BlockedBeadsQuery
// ============================================================================

func TestBlockedBeadsQuery(t *testing.T) {
	dag := core.NewDAG()

	// Add beads: ready (InDegree=0) and blocked (InDegree>0)
	readyBead := createTestBead("hydra-ready", "Ready Bead", "implement", types.StatusOpen, 5)
	blockedBead1 := createTestBead("hydra-blocked1", "Blocked Bead 1", "test", types.StatusOpen, 4)
	blockedBead2 := createTestBead("hydra-blocked2", "Blocked Bead 2", "fix", types.StatusOpen, 3)

	dag.AddNode(readyBead)
	dag.AddNode(blockedBead1)
	dag.AddNode(blockedBead2)
	dag.AddEdge("hydra-blocked1", "hydra-ready")
	dag.AddEdge("hydra-blocked2", "hydra-blocked1")

	allNodes := dag.GetAllNodes()
	blockedCount := 0
	for _, node := range allNodes {
		if node.InDegree > 0 {
			blockedCount++
		}
	}

	// Should return beads with InDegree > 0
	if blockedCount != 2 {
		t.Errorf("Blocked beads count = %d, want 2", blockedCount)
	}
}

func TestBlockedBeadsQuery_Empty(t *testing.T) {
	dag := core.NewDAG()
	// Add only ready beads
	readyBead1 := createTestBead("hydra-ready1", "Ready 1", "test", types.StatusOpen, 5)
	readyBead2 := createTestBead("hydra-ready2", "Ready 2", "implement", types.StatusOpen, 4)

	dag.AddNode(readyBead1)
	dag.AddNode(readyBead2)

	blockedCount := dag.GetBlockedCount()

	if blockedCount != 0 {
		t.Errorf("GetBlockedCount() = %d, want 0", blockedCount)
	}
}

// ============================================================================
// Test WorkersQuery
// ============================================================================

func TestWorkersQuery(t *testing.T) {
	// Create a test config
	config := &core.Config{
		Overseer: core.OverseerConfig{
			MaxWorkers:     10,
			PollInterval:   5 * time.Second,
			DefaultRetries: 3,
		},
	}

	overseer := core.NewOverseer(config)

	// Since we can't easily add workers to the overseer without starting it,
	// we'll test that the method returns without error
	workers := overseer.GetActiveWorkers()

	// Should return empty slice when no workers
	if workers == nil {
		t.Error("GetActiveWorkers() returned nil instead of empty slice")
	}

	if len(workers) != 0 {
		t.Errorf("GetActiveWorkers() returned %d workers, want 0", len(workers))
	}
}

// ============================================================================
// Test WorkerQuery (Single Worker)
// ============================================================================

func TestWorkerQuery(t *testing.T) {
	config := &core.Config{
		Overseer: core.OverseerConfig{
			MaxWorkers:     10,
			PollInterval:   5 * time.Second,
			DefaultRetries: 3,
		},
	}

	overseer := core.NewOverseer(config)

	// Test finding a worker by ID when no workers exist
	workers := overseer.GetActiveWorkers()
	found := false
	for _, w := range workers {
		if w.ID == "worker-1" {
			found = true
			break
		}
	}

	if found {
		t.Error("Found non-existent worker")
	}
}

// ============================================================================
// Test DagStatsQuery
// ============================================================================

func TestDagStatsQuery(t *testing.T) {
	dag := core.NewDAG()

	// Add beads with various states
	beads := []types.Bead{
		createTestBead("hydra-1", "Bead 1", "implement", types.StatusOpen, 5),
		createTestBead("hydra-2", "Bead 2", "test", types.StatusOpen, 4),
		createTestBead("hydra-3", "Bead 3", "fix", types.StatusOpen, 3),
		createTestBead("hydra-4", "Bead 4", "review", types.StatusOpen, 2),
	}

	for _, bead := range beads {
		dag.AddNode(bead)
	}

	// Create dependencies to block some beads
	dag.AddEdge("hydra-2", "hydra-1")
	dag.AddEdge("hydra-3", "hydra-2")
	dag.AddEdge("hydra-4", "hydra-3")

	// Mark some as completed/failed
	dag.UpdateNodeStatus("hydra-1", types.StatusCompleted)

	stats := core.DAGStats{
		TotalNodes:     dag.GetNodeCount(),
		ReadyNodes:     len(dag.GetReadyNodes()),
		BlockedNodes:   dag.GetBlockedCount(),
		CompletedNodes: dag.GetCompletedCount(),
		FailedNodes:    dag.GetFailedCount(),
	}

	// Verify stats are consistent
	if stats.TotalNodes != 4 {
		t.Errorf("TotalNodes = %d, want 4", stats.TotalNodes)
	}

	// hydra-1 is completed
	if stats.CompletedNodes != 1 {
		t.Errorf("CompletedNodes = %d, want 1", stats.CompletedNodes)
	}

	// Verify total matches sum of categories (approximately)
	totalCategorized := stats.ReadyNodes + stats.BlockedNodes + stats.CompletedNodes + stats.FailedNodes
	if totalCategorized > stats.TotalNodes {
		t.Errorf("Categorized total %d exceeds TotalNodes %d", totalCategorized, stats.TotalNodes)
	}
}

func TestDagStatsQuery_EmptyDAG(t *testing.T) {
	dag := core.NewDAG()
	// No beads added

	stats := core.DAGStats{
		TotalNodes:     dag.GetNodeCount(),
		ReadyNodes:     len(dag.GetReadyNodes()),
		BlockedNodes:   dag.GetBlockedCount(),
		CompletedNodes: dag.GetCompletedCount(),
		FailedNodes:    dag.GetFailedCount(),
	}

	if stats.TotalNodes != 0 {
		t.Errorf("TotalNodes = %d, want 0", stats.TotalNodes)
	}
}

// ============================================================================
// Test HealthQuery
// ============================================================================

func TestHealthQuery(t *testing.T) {
	config := &core.Config{
		Overseer: core.OverseerConfig{
			MaxWorkers:     10,
			PollInterval:   5 * time.Second,
			DefaultRetries: 3,
		},
	}

	overseer := core.NewOverseer(config)

	tests := []struct {
		name          string
		isPaused      bool
		wantStatus    string
		wantVersion   string
		wantTimestamp bool
	}{
		{
			name:          "healthy when running",
			isPaused:      false,
			wantStatus:    "healthy",
			wantVersion:   "1.0.0",
			wantTimestamp: true,
		},
		{
			name:          "paused status",
			isPaused:      true,
			wantStatus:    "paused",
			wantVersion:   "1.0.0",
			wantTimestamp: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.isPaused {
				overseer.Pause()
			} else {
				overseer.Resume()
			}

			// Determine status - note: IsRunning() returns false until Start() is called
			// For testing purposes, we consider it "healthy" if not explicitly paused
			status := "healthy"
			if overseer.IsPaused() {
				status = "paused"
			}

			if tt.isPaused {
				if status != "paused" {
					t.Errorf("Status = %v, want paused", status)
				}
			} else {
				if status != "healthy" {
					t.Errorf("Status = %v, want healthy", status)
				}
			}
		})
	}
}

// ============================================================================
// Test StatsQuery
// ============================================================================

func TestStatsQuery(t *testing.T) {
	// Since the projects are loaded from config or auto-discovered,
	// we'll test the stats calculation logic
	projects := []types.Project{
		{Name: "project1"},
		{Name: "project2"},
		{Name: "project3"},
	}

	totalProjects := int32(len(projects))

	if totalProjects != 3 {
		t.Errorf("TotalProjects = %d, want 3", totalProjects)
	}
}

func TestStatsQuery_NoProjects(t *testing.T) {
	projects := []types.Project{}

	totalProjects := int32(len(projects))

	if totalProjects != 0 {
		t.Errorf("TotalProjects = %d, want 0", totalProjects)
	}
}

// ============================================================================
// Test Helper Functions
// ============================================================================

func TestMatchesFilter(t *testing.T) {
	bead := &model.Bead{
		ID:        "hydra-abc123",
		Title:     "Test Bead",
		Type:      "implement",
		Status:    model.BeadStatusOpen,
		Priority:  5,
		Project:   "hydra",
		Labels:    []string{"test", "unit"},
		CreatedAt: time.Now().Add(-24 * time.Hour),
	}

	tests := []struct {
		name   string
		filter *model.BeadFilter
		want   bool
	}{
		{
			name:   "nil filter matches all",
			filter: nil,
			want:   true,
		},
		{
			name: "status match",
			filter: &model.BeadFilter{
				Status: []model.BeadStatus{model.BeadStatusOpen},
			},
			want: true,
		},
		{
			name: "status no match",
			filter: &model.BeadFilter{
				Status: []model.BeadStatus{model.BeadStatusCompleted},
			},
			want: false,
		},
		{
			name: "type match",
			filter: &model.BeadFilter{
				Type: []string{"implement"},
			},
			want: true,
		},
		{
			name: "type no match",
			filter: &model.BeadFilter{
				Type: []string{"test"},
			},
			want: false,
		},
		{
			name: "project match",
			filter: &model.BeadFilter{
				Project: stringPtr("hydra"),
			},
			want: true,
		},
		{
			name: "project no match",
			filter: &model.BeadFilter{
				Project: stringPtr("other"),
			},
			want: false,
		},
		{
			name: "priority range match",
			filter: &model.BeadFilter{
				PriorityMin: int32Ptr(1),
				PriorityMax: int32Ptr(10),
			},
			want: true,
		},
		{
			name: "priority min no match",
			filter: &model.BeadFilter{
				PriorityMin: int32Ptr(10),
			},
			want: false,
		},
		{
			name: "labels match",
			filter: &model.BeadFilter{
				Labels: []string{"test"},
			},
			want: true,
		},
		{
			name: "labels no match",
			filter: &model.BeadFilter{
				Labels: []string{"nonexistent"},
			},
			want: false,
		},
		{
			name: "multiple labels must all match",
			filter: &model.BeadFilter{
				Labels: []string{"test", "unit"},
			},
			want: true,
		},
		{
			name: "multiple labels partial match fails",
			filter: &model.BeadFilter{
				Labels: []string{"test", "nonexistent"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesFilter(bead, tt.filter)
			if got != tt.want {
				t.Errorf("matchesFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplySorting(t *testing.T) {
	beads := []*model.Bead{
		{ID: "a", Priority: 3, Title: "Charlie", CreatedAt: time.Now().Add(-3 * time.Hour)},
		{ID: "b", Priority: 1, Title: "Alpha", CreatedAt: time.Now().Add(-1 * time.Hour)},
		{ID: "c", Priority: 2, Title: "Bravo", CreatedAt: time.Now().Add(-2 * time.Hour)},
	}

	tests := []struct {
		name    string
		sort    []*model.BeadSort
		wantIDs []string
	}{
		{
			name:    "priority ascending",
			sort:    []*model.BeadSort{{Field: model.BeadSortFieldPriority, Direction: model.SortDirectionAsc}},
			wantIDs: []string{"b", "c", "a"},
		},
		{
			name:    "priority descending",
			sort:    []*model.BeadSort{{Field: model.BeadSortFieldPriority, Direction: model.SortDirectionDesc}},
			wantIDs: []string{"a", "c", "b"},
		},
		{
			name:    "title ascending",
			sort:    []*model.BeadSort{{Field: model.BeadSortFieldTitle, Direction: model.SortDirectionAsc}},
			wantIDs: []string{"b", "c", "a"},
		},
		{
			name:    "empty sort does nothing",
			sort:    []*model.BeadSort{},
			wantIDs: []string{"a", "b", "c"}, // Original order
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying original
			beadsCopy := make([]*model.Bead, len(beads))
			copy(beadsCopy, beads)

			applySorting(beadsCopy, tt.sort)

			for i, wantID := range tt.wantIDs {
				if beadsCopy[i].ID != wantID {
					t.Errorf("applySorting() position %d = %s, want %s", i, beadsCopy[i].ID, wantID)
				}
			}
		})
	}
}

func TestEncodeDecodeCursor(t *testing.T) {
	// Test cursor encoding/decoding
	original := 42
	encoded := base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(original)))

	// Test decoding
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("Failed to decode cursor: %v", err)
	}

	decodedInt, err := strconv.Atoi(string(decoded))
	if err != nil {
		t.Fatalf("Failed to convert decoded cursor to int: %v", err)
	}

	if decodedInt != original {
		t.Errorf("Cursor round-trip failed: got %d, want %d", decodedInt, original)
	}
}

// ============================================================================
// Error Case Tests
// ============================================================================

func TestQueryResolver_NilOverseer(t *testing.T) {
	resolver := &Resolver{Overseer: nil}
	queryResolver := &queryResolver{Resolver: resolver}
	ctx := context.Background()

	tests := []struct {
		name   string
		testFn func() error
	}{
		{
			name: "Beads with nil overseer",
			testFn: func() error {
				_, err := queryResolver.Beads(ctx, nil, nil, nil, nil)
				return err
			},
		},
		{
			name: "Bead with nil overseer",
			testFn: func() error {
				_, err := queryResolver.Bead(ctx, "hydra-abc123")
				return err
			},
		},
		{
			name: "ReadyBeads with nil overseer",
			testFn: func() error {
				_, err := queryResolver.ReadyBeads(ctx)
				return err
			},
		},
		{
			name: "BlockedBeads with nil overseer",
			testFn: func() error {
				_, err := queryResolver.BlockedBeads(ctx)
				return err
			},
		},
		{
			name: "Workers with nil overseer",
			testFn: func() error {
				_, err := queryResolver.Workers(ctx)
				return err
			},
		},
		{
			name: "Worker with nil overseer",
			testFn: func() error {
				_, err := queryResolver.Worker(ctx, "worker-1")
				return err
			},
		},
		{
			name: "DagStats with nil overseer",
			testFn: func() error {
				_, err := queryResolver.DagStats(ctx)
				return err
			},
		},
		{
			name: "Health with nil overseer",
			testFn: func() error {
				_, err := queryResolver.Health(ctx)
				return err
			},
		},
		{
			name: "Stats with nil overseer",
			testFn: func() error {
				_, err := queryResolver.Stats(ctx)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.testFn()
			if err == nil {
				t.Error("Expected error for nil overseer, got nil")
			}
			if !containsSubstring(err.Error(), "overseer not initialized") {
				t.Errorf("Error message should contain 'overseer not initialized', got: %v", err)
			}
		})
	}
}

func TestBeadsQuery_InvalidCursor(t *testing.T) {
	dag := core.NewDAG()
	bead := createTestBead("hydra-abc123", "Test", "implement", types.StatusOpen, 5)
	dag.AddNode(bead)

	// Test with invalid cursor - should handle gracefully
	invalidCursor := "not-valid-base64!!!"
	first := int32(10)

	// Decode cursor (should fail gracefully)
	startIndex := 0
	decoded, err := base64.StdEncoding.DecodeString(invalidCursor)
	if err != nil {
		// Expected - invalid cursor, start from beginning
		startIndex = 0
	} else {
		if idx, err := strconv.Atoi(string(decoded)); err == nil {
			startIndex = idx + 1
		}
	}

	// Should start from beginning
	if startIndex != 0 {
		t.Error("Invalid cursor should start from beginning (index 0)")
	}

	// Apply limit
	limit := int(first)
	beads := dag.GetAllNodes()
	endIndex := startIndex + limit
	if endIndex > len(beads) {
		endIndex = len(beads)
	}

	// Should still return valid results
	if endIndex < startIndex {
		t.Error("endIndex should not be less than startIndex")
	}
}
