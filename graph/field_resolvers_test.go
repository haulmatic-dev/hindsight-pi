package graph

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/graph/model"
	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ============================================================================
// MOCKS (with Field suffix to avoid conflicts with other test files)
// ============================================================================

// fieldMockOverseer is a mock implementation of the Overseer for field resolver testing
type fieldMockOverseer struct {
	mu sync.RWMutex

	// Mock data
	beads        map[string]types.Bead
	dependencies map[string][]string
	dependents   map[string][]string
	workers      []types.Worker

	// Behavior flags
	returnError bool
	nilDAG      bool
}

func newFieldMockOverseer() *fieldMockOverseer {
	return &fieldMockOverseer{
		beads:        make(map[string]types.Bead),
		dependencies: make(map[string][]string),
		dependents:   make(map[string][]string),
		workers:      make([]types.Worker, 0),
	}
}

func (m *fieldMockOverseer) GetBeadDependencies(beadID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.nilDAG {
		return nil, fmt.Errorf("DAG not initialized")
	}

	if m.returnError {
		return nil, fmt.Errorf("mock error")
	}

	if deps, exists := m.dependencies[beadID]; exists {
		return deps, nil
	}
	return []string{}, nil
}

func (m *fieldMockOverseer) GetBeadDependents(beadID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.nilDAG {
		return nil, fmt.Errorf("DAG not initialized")
	}

	if m.returnError {
		return nil, fmt.Errorf("mock error")
	}

	if deps, exists := m.dependents[beadID]; exists {
		return deps, nil
	}
	return []string{}, nil
}

func (m *fieldMockOverseer) GetBeadFromDAG(beadID string) (types.Bead, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.nilDAG {
		return types.Bead{}, fmt.Errorf("DAG not initialized")
	}

	if bead, exists := m.beads[beadID]; exists {
		return bead, nil
	}
	return types.Bead{}, fmt.Errorf("bead %s not found in DAG", beadID)
}

func (m *fieldMockOverseer) GetActiveWorkers() []types.Worker {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]types.Worker{}, m.workers...)
}

func (m *fieldMockOverseer) addBead(bead types.Bead) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beads[string(bead.ID)] = bead
}

func (m *fieldMockOverseer) setDependencies(beadID string, deps []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dependencies[beadID] = deps
}

func (m *fieldMockOverseer) setDependents(beadID string, deps []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dependents[beadID] = deps
}

func (m *fieldMockOverseer) addWorker(worker types.Worker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers = append(m.workers, worker)
}

	enabled     bool
	returnError bool
}

		enabled:  true,
	}
}

	return m.enabled
}

	if !m.enabled {
			Success: true,
			Data: struct {
				HistorySnippets []intelligence.Snippet `json:"history_snippets"`
			}{
				HistorySnippets: []intelligence.Snippet{},
			},
		}, nil
	}

	if m.returnError {
		return nil, fmt.Errorf("mock cass_memory error")
	}

	if m.response != nil {
		return m.response, nil
	}

		Success: true,
		Data: struct {
			HistorySnippets []intelligence.Snippet `json:"history_snippets"`
		}{
			HistorySnippets: []intelligence.Snippet{},
		},
	}, nil
}

// fieldMockTLDR is a mock implementation of the TLDRClient for testing
type fieldMockTLDR struct {
	returnError bool
	response    *intelligence.TLDRImpactResponse
}

func newFieldMockTLDR() *fieldMockTLDR {
	return &fieldMockTLDR{
		response: &intelligence.TLDRImpactResponse{},
	}
}

func (m *fieldMockTLDR) ImpactAnalysis(file string, depth int) (*intelligence.TLDRImpactResponse, error) {
	if m.returnError {
		return nil, fmt.Errorf("mock TLDR error")
	}
	if m.response != nil {
		return m.response, nil
	}
	return nil, fmt.Errorf("no mock response configured")
}

// ============================================================================
// TEST RESOLVER INTERFACES
// ============================================================================

type fieldOverseerInterface interface {
	GetBeadDependencies(beadID string) ([]string, error)
	GetBeadDependents(beadID string) ([]string, error)
	GetBeadFromDAG(beadID string) (types.Bead, error)
	GetActiveWorkers() []types.Worker
}

	IsEnabled() bool
}

type fieldTLDRInterface interface {
	ImpactAnalysis(file string, depth int) (*intelligence.TLDRImpactResponse, error)
}

// fieldTestBeadResolver wraps beadResolver with test-specific dependencies
type fieldTestBeadResolver struct {
	overseer   fieldOverseerInterface
	tldr       fieldTLDRInterface
}

func (r *fieldTestBeadResolver) Dependencies(ctx context.Context, obj *model.Bead) ([]*model.Bead, error) {
	if r.overseer == nil {
		return []*model.Bead{}, nil
	}

	depIDs, err := r.overseer.GetBeadDependencies(obj.ID)
	if err != nil {
		return []*model.Bead{}, nil
	}

	dependencies := make([]*model.Bead, 0, len(depIDs))
	for _, depID := range depIDs {
		bead, err := r.overseer.GetBeadFromDAG(depID)
		if err != nil {
			continue
		}
		beadModel := convertTypesBeadToModel(&bead)
		dependencies = append(dependencies, beadModel)
	}

	return dependencies, nil
}

func (r *fieldTestBeadResolver) Dependents(ctx context.Context, obj *model.Bead) ([]*model.Bead, error) {
	if r.overseer == nil {
		return []*model.Bead{}, nil
	}

	depIDs, err := r.overseer.GetBeadDependents(obj.ID)
	if err != nil {
		return []*model.Bead{}, nil
	}

	dependents := make([]*model.Bead, 0, len(depIDs))
	for _, depID := range depIDs {
		bead, err := r.overseer.GetBeadFromDAG(depID)
		if err != nil {
			continue
		}
		beadModel := convertTypesBeadToModel(&bead)
		dependents = append(dependents, beadModel)
	}

	return dependents, nil
}

func (r *fieldTestBeadResolver) Worker(ctx context.Context, obj *model.Bead) (*model.Worker, error) {
	if r.overseer == nil {
		return nil, nil
	}

	activeWorkers := r.overseer.GetActiveWorkers()

	for _, worker := range activeWorkers {
		if worker.ID == obj.ID {
			return convertTypesWorkerToModel(&worker), nil
		}
	}

	return nil, nil
}

func (r *fieldTestBeadResolver) Patterns(ctx context.Context, obj *model.Bead) ([]*model.Pattern, error) {
		return []*model.Pattern{}, nil
	}

	query := fmt.Sprintf("%s %s", obj.Type, obj.Title)
	if obj.Description != nil && *obj.Description != "" {
		query = fmt.Sprintf("%s %s", query, *obj.Description)
	}

	if err != nil {
		return []*model.Pattern{}, nil
	}

	patterns := make([]*model.Pattern, 0, len(response.Data.RelevantBullets))
	for _, bullet := range response.Data.RelevantBullets {
		score := bullet.Score
		patterns = append(patterns, &model.Pattern{
			ID:       bullet.ID,
			Content:  bullet.Content,
			Category: bullet.Category,
			Score:    &score,
		})
	}

	return patterns, nil
}

func (r *fieldTestBeadResolver) Impact(ctx context.Context, obj *model.Bead) (*model.ImpactAnalysis, error) {
	if r.tldr == nil {
		return nil, nil
	}

	files := extractTargetFiles(obj)
	if len(files) == 0 {
		return nil, nil
	}

	targetFile := files[0]

	// Check cache first
	impactCacheMu.RLock()
	if cached, exists := impactCache[obj.ID]; exists && time.Now().Before(cached.expiration) {
		impactCacheMu.RUnlock()
		return convertTLDRResponseToModel(cached.analysis), nil
	}
	impactCacheMu.RUnlock()

	analysis, err := r.tldr.ImpactAnalysis(targetFile, 2)
	if err != nil {
		return nil, nil
	}

	impactCacheMu.Lock()
	impactCache[obj.ID] = &impactCacheEntry{
		analysis:   analysis,
		expiration: time.Now().Add(cacheTTL),
	}
	impactCacheMu.Unlock()

	return convertTLDRResponseToModel(analysis), nil
}

// fieldTestWorkerResolver wraps workerResolver with test-specific dependencies
type fieldTestWorkerResolver struct {
	overseer fieldOverseerInterface
}

func (r *fieldTestWorkerResolver) Bead(ctx context.Context, obj *model.Worker) (*model.Bead, error) {
	if r.overseer == nil {
		return nil, fmt.Errorf("overseer not available")
	}

	beadID := obj.ID
	bead, err := r.overseer.GetBeadFromDAG(beadID)
	if err != nil {
		return nil, nil
	}

	return convertTypesBeadToModel(&bead), nil
}

func (r *fieldTestWorkerResolver) Logs(ctx context.Context, obj *model.Worker) ([]*model.LogEntry, error) {
	return []*model.LogEntry{}, nil
}

// ============================================================================
// TEST CASES
// ============================================================================

func TestBeadDependencies(t *testing.T) {
	tests := []struct {
		name          string
		setupMocks    func(*fieldMockOverseer)
		bead          *model.Bead
		expectedCount int
		expectedIDs   []string
	}{
		{
			name: "returns dependency beads",
			setupMocks: func(m *fieldMockOverseer) {
				// Create beads with dependencies
				beadA := types.Bead{
					ID:      "test-project-A",
					Title:   "Bead A",
					Project: "test-project",
					Status:  types.StatusOpen,
				}
				beadB := types.Bead{
					ID:           "test-project-B",
					Title:        "Bead B",
					Project:      "test-project",
					Status:       types.StatusOpen,
					Dependencies: []types.BeadID{"test-project-A"},
				}
				m.addBead(beadA)
				m.addBead(beadB)
				m.setDependencies("test-project-B", []string{"test-project-A"})
			},
			bead: &model.Bead{
				ID:      "test-project-B",
				Title:   "Bead B",
				Project: "test-project",
			},
			expectedCount: 1,
			expectedIDs:   []string{"test-project-A"},
		},
		{
			name: "returns multiple dependencies",
			setupMocks: func(m *fieldMockOverseer) {
				beadA := types.Bead{ID: "test-project-A", Title: "A", Project: "test-project", Status: types.StatusOpen}
				beadB := types.Bead{ID: "test-project-B", Title: "B", Project: "test-project", Status: types.StatusOpen}
				beadC := types.Bead{
					ID:           "test-project-C",
					Title:        "C",
					Project:      "test-project",
					Status:       types.StatusOpen,
					Dependencies: []types.BeadID{"test-project-A", "test-project-B"},
				}
				m.addBead(beadA)
				m.addBead(beadB)
				m.addBead(beadC)
				m.setDependencies("test-project-C", []string{"test-project-A", "test-project-B"})
			},
			bead: &model.Bead{
				ID:      "test-project-C",
				Title:   "C",
				Project: "test-project",
			},
			expectedCount: 2,
			expectedIDs:   []string{"test-project-A", "test-project-B"},
		},
		{
			name: "empty when no dependencies",
			setupMocks: func(m *fieldMockOverseer) {
				beadA := types.Bead{
					ID:      "test-project-A",
					Title:   "Bead A",
					Project: "test-project",
					Status:  types.StatusOpen,
				}
				m.addBead(beadA)
				m.setDependencies("test-project-A", []string{})
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectedCount: 0,
			expectedIDs:   []string{},
		},
		{
			name: "handles circular dependency gracefully",
			setupMocks: func(m *fieldMockOverseer) {
				// A depends on B, B depends on A (circular)
				beadA := types.Bead{
					ID:           "test-project-A",
					Title:        "Bead A",
					Project:      "test-project",
					Status:       types.StatusOpen,
					Dependencies: []types.BeadID{"test-project-B"},
				}
				beadB := types.Bead{
					ID:           "test-project-B",
					Title:        "Bead B",
					Project:      "test-project",
					Status:       types.StatusOpen,
					Dependencies: []types.BeadID{"test-project-A"},
				}
				m.addBead(beadA)
				m.addBead(beadB)
				m.setDependencies("test-project-A", []string{"test-project-B"})
				m.setDependencies("test-project-B", []string{"test-project-A"})
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectedCount: 1,
			expectedIDs:   []string{"test-project-B"},
		},
		{
			name: "returns empty when overseer returns error",
			setupMocks: func(m *fieldMockOverseer) {
				m.returnError = true
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectedCount: 0,
			expectedIDs:   []string{},
		},
		{
			name: "handles missing dependency bead gracefully",
			setupMocks: func(m *fieldMockOverseer) {
				beadA := types.Bead{
					ID:      "test-project-A",
					Title:   "Bead A",
					Project: "test-project",
					Status:  types.StatusOpen,
				}
				m.addBead(beadA)
				// Set dependency to a bead that doesn't exist in DAG
				m.setDependencies("test-project-A", []string{"test-project-missing"})
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectedCount: 0,
			expectedIDs:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOverseer := newFieldMockOverseer()
			tt.setupMocks(mockOverseer)

			resolver := &fieldTestBeadResolver{
				overseer: fieldOverseerInterface(mockOverseer),
			}

			ctx := context.Background()
			dependencies, err := resolver.Dependencies(ctx, tt.bead)

			if err != nil {
				t.Fatalf("Dependencies() unexpected error: %v", err)
			}

			if len(dependencies) != tt.expectedCount {
				t.Errorf("Dependencies() returned %d dependencies, expected %d", len(dependencies), tt.expectedCount)
			}

			for i, expectedID := range tt.expectedIDs {
				if i < len(dependencies) && dependencies[i].ID != expectedID {
					t.Errorf("Dependencies()[%d].ID = %s, expected %s", i, dependencies[i].ID, expectedID)
				}
			}
		})
	}
}

func TestBeadDependents(t *testing.T) {
	tests := []struct {
		name          string
		setupMocks    func(*fieldMockOverseer)
		bead          *model.Bead
		expectedCount int
		expectedIDs   []string
	}{
		{
			name: "returns dependent beads",
			setupMocks: func(m *fieldMockOverseer) {
				// A is depended on by B
				beadA := types.Bead{
					ID:      "test-project-A",
					Title:   "Bead A",
					Project: "test-project",
					Status:  types.StatusOpen,
				}
				beadB := types.Bead{
					ID:           "test-project-B",
					Title:        "Bead B",
					Project:      "test-project",
					Status:       types.StatusOpen,
					Dependencies: []types.BeadID{"test-project-A"},
				}
				m.addBead(beadA)
				m.addBead(beadB)
				m.setDependents("test-project-A", []string{"test-project-B"})
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectedCount: 1,
			expectedIDs:   []string{"test-project-B"},
		},
		{
			name: "returns multiple dependents",
			setupMocks: func(m *fieldMockOverseer) {
				// A is depended on by B and C
				beadA := types.Bead{ID: "test-project-A", Title: "A", Project: "test-project", Status: types.StatusOpen}
				beadB := types.Bead{ID: "test-project-B", Title: "B", Project: "test-project", Status: types.StatusOpen, Dependencies: []types.BeadID{"test-project-A"}}
				beadC := types.Bead{ID: "test-project-C", Title: "C", Project: "test-project", Status: types.StatusOpen, Dependencies: []types.BeadID{"test-project-A"}}
				m.addBead(beadA)
				m.addBead(beadB)
				m.addBead(beadC)
				m.setDependents("test-project-A", []string{"test-project-B", "test-project-C"})
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "A",
				Project: "test-project",
			},
			expectedCount: 2,
			expectedIDs:   []string{"test-project-B", "test-project-C"},
		},
		{
			name: "empty when no dependents",
			setupMocks: func(m *fieldMockOverseer) {
				beadA := types.Bead{
					ID:      "test-project-A",
					Title:   "Bead A",
					Project: "test-project",
					Status:  types.StatusOpen,
				}
				m.addBead(beadA)
				m.setDependents("test-project-A", []string{})
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectedCount: 0,
			expectedIDs:   []string{},
		},
		{
			name: "diamond pattern dependents",
			setupMocks: func(m *fieldMockOverseer) {
				//     A
				//    / \
				//   B   C
				//    \ /
				//     D
				// A is depended on by B and C
				beadA := types.Bead{ID: "test-project-A", Title: "A", Project: "test-project", Status: types.StatusOpen}
				beadB := types.Bead{ID: "test-project-B", Title: "B", Project: "test-project", Status: types.StatusOpen, Dependencies: []types.BeadID{"test-project-A"}}
				beadC := types.Bead{ID: "test-project-C", Title: "C", Project: "test-project", Status: types.StatusOpen, Dependencies: []types.BeadID{"test-project-A"}}
				m.addBead(beadA)
				m.addBead(beadB)
				m.addBead(beadC)
				m.setDependents("test-project-A", []string{"test-project-B", "test-project-C"})
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "A",
				Project: "test-project",
			},
			expectedCount: 2,
			expectedIDs:   []string{"test-project-B", "test-project-C"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOverseer := newFieldMockOverseer()
			tt.setupMocks(mockOverseer)

			resolver := &fieldTestBeadResolver{
				overseer: fieldOverseerInterface(mockOverseer),
			}

			ctx := context.Background()
			dependents, err := resolver.Dependents(ctx, tt.bead)

			if err != nil {
				t.Fatalf("Dependents() unexpected error: %v", err)
			}

			if len(dependents) != tt.expectedCount {
				t.Errorf("Dependents() returned %d dependents, expected %d", len(dependents), tt.expectedCount)
			}

			for i, expectedID := range tt.expectedIDs {
				if i < len(dependents) && dependents[i].ID != expectedID {
					t.Errorf("Dependents()[%d].ID = %s, expected %s", i, dependents[i].ID, expectedID)
				}
			}
		})
	}
}

func TestBeadWorker(t *testing.T) {
	tests := []struct {
		name         string
		setupMocks   func(*fieldMockOverseer)
		bead         *model.Bead
		expectWorker bool
		expectedID   string
	}{
		{
			name: "returns active worker for bead",
			setupMocks: func(m *fieldMockOverseer) {
				worker := types.Worker{
					ID:        "test-project-A",
					BeadID:    "test-project-A",
					PID:       12345,
					AgentType: types.AgentOrchestrator,
					Status:    "running",
				}
				m.addWorker(worker)
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectWorker: true,
			expectedID:   "test-project-A",
		},
		{
			name: "nil when no worker assigned",
			setupMocks: func(m *fieldMockOverseer) {
				// No workers added
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectWorker: false,
		},
		{
			name: "returns nil for different bead ID",
			setupMocks: func(m *fieldMockOverseer) {
				worker := types.Worker{
					ID:        "test-project-B",
					BeadID:    "test-project-B",
					PID:       12345,
					AgentType: types.AgentOrchestrator,
					Status:    "running",
				}
				m.addWorker(worker)
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectWorker: false,
		},
		{
			name: "returns correct worker among multiple",
			setupMocks: func(m *fieldMockOverseer) {
				workerA := types.Worker{ID: "test-project-A", BeadID: "test-project-A", PID: 12345, AgentType: types.AgentOrchestrator, Status: "running"}
				workerB := types.Worker{ID: "test-project-B", BeadID: "test-project-B", PID: 12346, AgentType: types.AgentOrchestrator, Status: "running"}
				workerC := types.Worker{ID: "test-project-C", BeadID: "test-project-C", PID: 12347, AgentType: types.AgentOrchestrator, Status: "running"}
				m.addWorker(workerA)
				m.addWorker(workerB)
				m.addWorker(workerC)
			},
			bead: &model.Bead{
				ID:      "test-project-B",
				Title:   "Bead B",
				Project: "test-project",
			},
			expectWorker: true,
			expectedID:   "test-project-B",
		},
		{
			name: "handles nil overseer gracefully",
			setupMocks: func(m *fieldMockOverseer) {
				// Don't set up any mocks - we'll pass nil overseer
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Bead A",
				Project: "test-project",
			},
			expectWorker: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resolver *fieldTestBeadResolver

			if tt.name == "handles nil overseer gracefully" {
				resolver = &fieldTestBeadResolver{
					overseer: nil,
				}
			} else {
				mockOverseer := newFieldMockOverseer()
				tt.setupMocks(mockOverseer)
				resolver = &fieldTestBeadResolver{
					overseer: fieldOverseerInterface(mockOverseer),
				}
			}

			ctx := context.Background()
			worker, err := resolver.Worker(ctx, tt.bead)

			if err != nil {
				t.Fatalf("Worker() unexpected error: %v", err)
			}

			if tt.expectWorker {
				if worker == nil {
					t.Error("Worker() returned nil, expected a worker")
				} else if worker.ID != tt.expectedID {
					t.Errorf("Worker().ID = %s, expected %s", worker.ID, tt.expectedID)
				}
			} else {
				if worker != nil {
					t.Errorf("Worker() returned a worker (ID: %s), expected nil", worker.ID)
				}
			}
		})
	}
}

func TestBeadPatterns(t *testing.T) {
	tests := []struct {
		name          string
		bead          *model.Bead
		expectedCount int
		expectedIDs   []string
	}{
		{
			name: "queries cass_memory and returns patterns",
				m.enabled = true
					Success: true,
					Data: struct {
						HistorySnippets []intelligence.Snippet `json:"history_snippets"`
					}{
							{ID: "bullet-1", Content: "Pattern 1", Category: "testing", Score: 0.95},
							{ID: "bullet-2", Content: "Pattern 2", Category: "testing", Score: 0.85},
						},
					},
				}
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Test Bead",
				Type:    "test",
				Project: "test-project",
			},
			expectedCount: 2,
			expectedIDs:   []string{"bullet-1", "bullet-2"},
		},
		{
			name: "returns empty when cass_memory unavailable",
				m.enabled = false
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Test Bead",
				Type:    "test",
				Project: "test-project",
			},
			expectedCount: 0,
		},
		{
			name: "returns empty when cass_memory returns error",
				m.enabled = true
				m.returnError = true
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Test Bead",
				Type:    "test",
				Project: "test-project",
			},
			expectedCount: 0,
		},
		{
			name: "returns patterns with scores",
				m.enabled = true
					Success: true,
					Data: struct {
						HistorySnippets []intelligence.Snippet `json:"history_snippets"`
					}{
							{ID: "bullet-1", Content: "High relevance", Category: "review", Score: 0.99},
							{ID: "bullet-2", Content: "Medium relevance", Category: "review", Score: 0.75},
							{ID: "bullet-3", Content: "Low relevance", Category: "review", Score: 0.50},
						},
					},
				}
			},
			bead: &model.Bead{
				ID:      "test-project-A",
				Title:   "Review Bead",
				Type:    "review",
				Project: "test-project",
			},
			expectedCount: 3,
			expectedIDs:   []string{"bullet-1", "bullet-2", "bullet-3"},
		},
		{
			name: "includes description in query",
				m.enabled = true
					Success: true,
					Data: struct {
						HistorySnippets []intelligence.Snippet `json:"history_snippets"`
					}{
							{ID: "bullet-1", Content: "Pattern for database queries", Category: "database", Score: 0.90},
						},
					},
				}
			},
			bead: &model.Bead{
				ID:          "test-project-A",
				Title:       "Database Migration",
				Type:        "database",
				Project:     "test-project",
				Description: fieldStrPtr("Create users table with indexes"),
			},
			expectedCount: 1,
			expectedIDs:   []string{"bullet-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			resolver := &fieldTestBeadResolver{
			}

			ctx := context.Background()
			patterns, err := resolver.Patterns(ctx, tt.bead)

			if err != nil {
				t.Fatalf("Patterns() unexpected error: %v", err)
			}

			if len(patterns) != tt.expectedCount {
				t.Errorf("Patterns() returned %d patterns, expected %d", len(patterns), tt.expectedCount)
			}

			for i, expectedID := range tt.expectedIDs {
				if i < len(patterns) && patterns[i].ID != expectedID {
					t.Errorf("Patterns()[%d].ID = %s, expected %s", i, patterns[i].ID, expectedID)
				}
			}

			// Verify scores are present
			for i, pattern := range patterns {
				if pattern.Score == nil {
					t.Errorf("Patterns()[%d].Score is nil, expected a value", i)
				}
			}
		})
	}
}

func TestBeadImpact(t *testing.T) {
	// Clear cache before each test
	impactCacheMu.Lock()
	impactCache = make(map[string]*impactCacheEntry)
	impactCacheMu.Unlock()

	tests := []struct {
		name           string
		setupMocks     func(*fieldMockTLDR)
		bead           *model.Bead
		expectedNil    bool
		expectedFiles  int32
		expectedTokens int32
		checkCache     bool
	}{
		{
			name: "returns impact analysis",
			setupMocks: func(m *fieldMockTLDR) {
				m.response = &intelligence.TLDRImpactResponse{
					File: "internal/core/config.go",
					Impact: struct {
						Callers []intelligence.Dependency `json:"callers"`
						Callees []intelligence.Dependency `json:"callees"`
					}{
						Callers: []intelligence.Dependency{
							{File: "main.go", Function: "main", Line: 10},
						},
						Callees: []intelligence.Dependency{
							{File: "internal/utils/helper.go", Function: "HelperFunc", Line: 25},
						},
					},
					Scope: struct {
						Files    int  `json:"files"`
						Tokens   int  `json:"tokens"`
						Exceeded bool `json:"exceeded"`
					}{
						Files:    5,
						Tokens:   2500,
						Exceeded: false,
					},
				}
			},
			bead: &model.Bead{
				ID:          "test-project-A",
				Title:       "Modify config.go",
				Type:        "implement",
				Project:     "test-project",
				Description: fieldStrPtr("Update internal/core/config.go to add new settings"),
			},
			expectedNil:    false,
			expectedFiles:  5,
			expectedTokens: 2500,
		},
		{
			name: "nil when TLDR unavailable",
			setupMocks: func(m *fieldMockTLDR) {
				m.returnError = true
			},
			bead: &model.Bead{
				ID:          "test-project-A",
				Title:       "Modify config.go",
				Type:        "implement",
				Project:     "test-project",
				Description: fieldStrPtr("Update internal/core/config.go"),
			},
			expectedNil: true,
		},
		{
			name: "nil when no files in description",
			setupMocks: func(m *fieldMockTLDR) {
				m.response = &intelligence.TLDRImpactResponse{
					File: "",
					Scope: struct {
						Files    int  `json:"files"`
						Tokens   int  `json:"tokens"`
						Exceeded bool `json:"exceeded"`
					}{
						Files:    0,
						Tokens:   0,
						Exceeded: false,
					},
				}
			},
			bead: &model.Bead{
				ID:          "test-project-A",
				Title:       "Update documentation",
				Type:        "documentation",
				Project:     "test-project",
				Description: fieldStrPtr("Improve the README with better examples"),
			},
			expectedNil: true,
		},
		{
			name: "caching works - first call stores in cache",
			setupMocks: func(m *fieldMockTLDR) {
				m.response = &intelligence.TLDRImpactResponse{
					File: "api/server.go",
					Scope: struct {
						Files    int  `json:"files"`
						Tokens   int  `json:"tokens"`
						Exceeded bool `json:"exceeded"`
					}{
						Files:    3,
						Tokens:   1500,
						Exceeded: false,
					},
				}
			},
			bead: &model.Bead{
				ID:          "test-project-B",
				Title:       "Modify server.go",
				Type:        "implement",
				Project:     "test-project",
				Description: fieldStrPtr("Update api/server.go"),
			},
			expectedNil:    false,
			expectedFiles:  3,
			expectedTokens: 1500,
			checkCache:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear cache before test
			impactCacheMu.Lock()
			impactCache = make(map[string]*impactCacheEntry)
			impactCacheMu.Unlock()

			mockTLDR := newFieldMockTLDR()
			tt.setupMocks(mockTLDR)

			resolver := &fieldTestBeadResolver{
				tldr: fieldTLDRInterface(mockTLDR),
			}

			ctx := context.Background()
			impact, err := resolver.Impact(ctx, tt.bead)

			if err != nil {
				t.Fatalf("Impact() unexpected error: %v", err)
			}

			if tt.expectedNil {
				if impact != nil {
					t.Errorf("Impact() returned non-nil, expected nil")
				}
			} else {
				if impact == nil {
					t.Fatal("Impact() returned nil, expected impact analysis")
				}
				if impact.Files != tt.expectedFiles {
					t.Errorf("Impact().Files = %d, expected %d", impact.Files, tt.expectedFiles)
				}
				if impact.Tokens != tt.expectedTokens {
					t.Errorf("Impact().Tokens = %d, expected %d", impact.Tokens, tt.expectedTokens)
				}
			}

			// Check cache if needed
			if tt.checkCache {
				impactCacheMu.RLock()
				cached, exists := impactCache[tt.bead.ID]
				impactCacheMu.RUnlock()

				if !exists {
					t.Error("Expected impact to be cached, but not found in cache")
				} else if cached.analysis == nil {
					t.Error("Cached analysis is nil")
				} else if cached.expiration.Before(time.Now()) {
					t.Error("Cached entry is already expired")
				}
			}
		})
	}
}

func TestBeadImpactCaching(t *testing.T) {
	// Test that caching actually returns cached value on second call
	impactCacheMu.Lock()
	impactCache = make(map[string]*impactCacheEntry)
	impactCacheMu.Unlock()

	mockTLDR := newFieldMockTLDR()
	callCount := 0

	// Create a custom mock that tracks call count
	trackingMock := &fieldTrackingTLDRMock{
		mockTLDR:  mockTLDR,
		callCount: &callCount,
	}

	resolver := &fieldTestBeadResolver{
		tldr: trackingMock,
	}

	bead := &model.Bead{
		ID:          "test-project-cache",
		Title:       "Test Caching",
		Type:        "implement",
		Project:     "test-project",
		Description: fieldStrPtr("Update internal/test.go"),
	}

	ctx := context.Background()

	// First call - should hit TLDR
	_, err := resolver.Impact(ctx, bead)
	if err != nil {
		t.Fatalf("First Impact() call failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected 1 TLDR call after first Impact(), got %d", callCount)
	}

	// Second call - should use cache
	_, err = resolver.Impact(ctx, bead)
	if err != nil {
		t.Fatalf("Second Impact() call failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected 1 TLDR call after second Impact() (cache hit), got %d", callCount)
	}
}

// fieldTrackingTLDRMock wraps fieldMockTLDR to track call counts
type fieldTrackingTLDRMock struct {
	mockTLDR  *fieldMockTLDR
	callCount *int
}

func (t *fieldTrackingTLDRMock) ImpactAnalysis(file string, depth int) (*intelligence.TLDRImpactResponse, error) {
	*t.callCount++
	return t.mockTLDR.ImpactAnalysis(file, depth)
}

func TestWorkerBead(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*fieldMockOverseer)
		worker      *model.Worker
		expectBead  bool
		expectedID  string
		expectError bool
	}{
		{
			name: "returns bead being processed",
			setupMocks: func(m *fieldMockOverseer) {
				bead := types.Bead{
					ID:      "test-project-A",
					Title:   "Bead A",
					Project: "test-project",
					Status:  types.StatusOpen,
					Type:    "implement",
				}
				m.addBead(bead)
			},
			worker: &model.Worker{
				ID:        "test-project-A",
				AgentType: "orchestrator",
				Status:    model.WorkerStatusRunning,
			},
			expectBead:  true,
			expectedID:  "test-project-A",
			expectError: false,
		},
		{
			name: "returns correct bead",
			setupMocks: func(m *fieldMockOverseer) {
				beadA := types.Bead{
					ID:      "test-project-A",
					Title:   "Bead A",
					Project: "test-project",
					Status:  types.StatusOpen,
					Type:    "implement",
				}
				beadB := types.Bead{
					ID:      "test-project-B",
					Title:   "Bead B",
					Project: "test-project",
					Status:  types.StatusOpen,
					Type:    "test",
				}
				m.addBead(beadA)
				m.addBead(beadB)
			},
			worker: &model.Worker{
				ID:        "test-project-B",
				AgentType: "test-specialist",
				Status:    model.WorkerStatusRunning,
			},
			expectBead:  true,
			expectedID:  "test-project-B",
			expectError: false,
		},
		{
			name: "returns nil when bead not found",
			setupMocks: func(m *fieldMockOverseer) {
				// No beads added
			},
			worker: &model.Worker{
				ID:        "test-project-missing",
				AgentType: "orchestrator",
				Status:    model.WorkerStatusRunning,
			},
			expectBead:  false,
			expectError: false,
		},
		{
			name: "returns error when overseer nil",
			setupMocks: func(m *fieldMockOverseer) {
				// Don't set up anything
			},
			worker: &model.Worker{
				ID:        "test-project-A",
				AgentType: "orchestrator",
				Status:    model.WorkerStatusRunning,
			},
			expectBead:  false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resolver *fieldTestWorkerResolver

			if tt.name == "returns error when overseer nil" {
				resolver = &fieldTestWorkerResolver{
					overseer: nil,
				}
			} else {
				mockOverseer := newFieldMockOverseer()
				tt.setupMocks(mockOverseer)
				resolver = &fieldTestWorkerResolver{
					overseer: fieldOverseerInterface(mockOverseer),
				}
			}

			ctx := context.Background()
			bead, err := resolver.Bead(ctx, tt.worker)

			if tt.expectError {
				if err == nil {
					t.Error("Bead() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Bead() unexpected error: %v", err)
				}

				if tt.expectBead {
					if bead == nil {
						t.Error("Bead() returned nil, expected a bead")
					} else if bead.ID != tt.expectedID {
						t.Errorf("Bead().ID = %s, expected %s", bead.ID, tt.expectedID)
					}
				} else {
					if bead != nil {
						t.Errorf("Bead() returned a bead (ID: %s), expected nil", bead.ID)
					}
				}
			}
		})
	}
}

func TestWorkerLogs(t *testing.T) {
	tests := []struct {
		name          string
		worker        *model.Worker
		expectedCount int
	}{
		{
			name: "returns empty logs",
			worker: &model.Worker{
				ID:        "test-project-A",
				AgentType: "orchestrator",
				Status:    model.WorkerStatusRunning,
			},
			expectedCount: 0,
		},
		{
			name: "empty when no logs",
			worker: &model.Worker{
				ID:        "test-project-B",
				AgentType: "test-specialist",
				Status:    model.WorkerStatusRunning,
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fieldTestWorkerResolver{}

			ctx := context.Background()
			logs, err := resolver.Logs(ctx, tt.worker)

			if err != nil {
				t.Fatalf("Logs() unexpected error: %v", err)
			}

			if len(logs) != tt.expectedCount {
				t.Errorf("Logs() returned %d logs, expected %d", len(logs), tt.expectedCount)
			}
		})
	}
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func fieldStrPtr(s string) *string {
	return &s
}

// TestBeadDependenciesWithOverseerNil tests the actual production behavior
func TestBeadDependenciesWithOverseerNil(t *testing.T) {
	resolver := &beadResolver{
		Resolver: &Resolver{
			Overseer: nil,
		},
	}

	bead := &model.Bead{
		ID:      "test-project-A",
		Title:   "Test Bead",
		Project: "test-project",
	}

	ctx := context.Background()
	dependencies, err := resolver.Dependencies(ctx, bead)

	if err != nil {
		t.Fatalf("Dependencies() unexpected error: %v", err)
	}

	if dependencies == nil || len(dependencies) != 0 {
		t.Errorf("Expected empty slice when overseer is nil, got %v", dependencies)
	}
}

	resolver := &beadResolver{
		Resolver: &Resolver{
		},
	}

	bead := &model.Bead{
		ID:      "test-project-A",
		Title:   "Test Bead",
		Type:    "implement",
		Project: "test-project",
	}

	ctx := context.Background()
	patterns, err := resolver.Patterns(ctx, bead)

	if err != nil {
		t.Fatalf("Patterns() unexpected error: %v", err)
	}

	if patterns == nil || len(patterns) != 0 {
		t.Errorf("Expected empty slice when cass_memory is nil, got %v", patterns)
	}
}

// TestBeadImpactWithTLDRNil tests the actual production behavior
func TestBeadImpactWithTLDRNil(t *testing.T) {
	resolver := &beadResolver{
		Resolver: &Resolver{
			TLDR: nil,
		},
	}

	desc := "Update internal/core/config.go"
	bead := &model.Bead{
		ID:          "test-project-A",
		Title:       "Test Bead",
		Type:        "implement",
		Project:     "test-project",
		Description: &desc,
	}

	ctx := context.Background()
	impact, err := resolver.Impact(ctx, bead)

	if err != nil {
		t.Fatalf("Impact() unexpected error: %v", err)
	}

	if impact != nil {
		t.Errorf("Expected nil when TLDR is nil, got %v", impact)
	}
}

// TestExtractTargetFiles tests the file extraction helper function
func TestExtractTargetFiles(t *testing.T) {
	tests := []struct {
		name          string
		description   string
		expectedFiles []string
	}{
		{
			name:          "extracts single file path",
			description:   "Update internal/core/config.go with new settings",
			expectedFiles: []string{"internal/core/config.go"},
		},
		{
			name:          "extracts multiple files",
			description:   "Modify api/server.go and internal/utils/helper.go",
			expectedFiles: []string{"api/server.go", "internal/utils/helper.go"},
		},
		{
			name:          "extracts files with various extensions",
			description:   "Update app.js, test.py, and config.json",
			expectedFiles: []string{"app.js", "test.py", "config.json"},
		},
		{
			name:          "returns empty for no files",
			description:   "Improve the documentation and add examples",
			expectedFiles: []string{},
		},
		{
			name:          "handles mixed content",
			description:   "Fix the bug in auth.go where tokens are not validated properly",
			expectedFiles: []string{"auth.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bead := &model.Bead{
				Description: &tt.description,
			}

			files := extractTargetFiles(bead)

			if len(files) != len(tt.expectedFiles) {
				t.Errorf("extractTargetFiles() returned %d files, expected %d: %v", len(files), len(tt.expectedFiles), files)
				return
			}

			for i, expected := range tt.expectedFiles {
				if i < len(files) && files[i] != expected {
					t.Errorf("extractTargetFiles()[%d] = %s, expected %s", i, files[i], expected)
				}
			}
		})
	}
}

// TestConvertTLDRResponseToModel tests the conversion function
func TestConvertTLDRResponseToModel(t *testing.T) {
	tests := []struct {
		name     string
		input    *intelligence.TLDRImpactResponse
		expected *model.ImpactAnalysis
	}{
		{
			name: "converts full response",
			input: &intelligence.TLDRImpactResponse{
				File: "main.go",
				Impact: struct {
					Callers []intelligence.Dependency `json:"callers"`
					Callees []intelligence.Dependency `json:"callees"`
				}{
					Callers: []intelligence.Dependency{
						{File: "cmd/main.go", Function: "main", Line: 10},
					},
					Callees: []intelligence.Dependency{
						{File: "pkg/utils.go", Function: "Helper", Line: 25},
					},
				},
				Scope: struct {
					Files    int  `json:"files"`
					Tokens   int  `json:"tokens"`
					Exceeded bool `json:"exceeded"`
				}{
					Files:    10,
					Tokens:   5000,
					Exceeded: false,
				},
			},
			expected: &model.ImpactAnalysis{
				Files:    10,
				Tokens:   5000,
				Exceeded: false,
				Callers: []*model.Dependency{
					{File: "cmd/main.go", Function: fieldStrPtr("main"), Line: fieldInt32Ptr(10)},
				},
				Callees: []*model.Dependency{
					{File: "pkg/utils.go", Function: fieldStrPtr("Helper"), Line: fieldInt32Ptr(25)},
				},
			},
		},
		{
			name:     "handles nil input",
			input:    nil,
			expected: nil,
		},
		{
			name: "handles empty dependencies",
			input: &intelligence.TLDRImpactResponse{
				File: "simple.go",
				Impact: struct {
					Callers []intelligence.Dependency `json:"callers"`
					Callees []intelligence.Dependency `json:"callees"`
				}{
					Callers: []intelligence.Dependency{},
					Callees: []intelligence.Dependency{},
				},
				Scope: struct {
					Files    int  `json:"files"`
					Tokens   int  `json:"tokens"`
					Exceeded bool `json:"exceeded"`
				}{
					Files:    1,
					Tokens:   100,
					Exceeded: false,
				},
			},
			expected: &model.ImpactAnalysis{
				Files:    1,
				Tokens:   100,
				Exceeded: false,
				Callers:  []*model.Dependency{},
				Callees:  []*model.Dependency{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertTLDRResponseToModel(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("Expected non-nil result, got nil")
			}

			if result.Files != tt.expected.Files {
				t.Errorf("Files = %d, expected %d", result.Files, tt.expected.Files)
			}
			if result.Tokens != tt.expected.Tokens {
				t.Errorf("Tokens = %d, expected %d", result.Tokens, tt.expected.Tokens)
			}
			if result.Exceeded != tt.expected.Exceeded {
				t.Errorf("Exceeded = %v, expected %v", result.Exceeded, tt.expected.Exceeded)
			}
			if len(result.Callers) != len(tt.expected.Callers) {
				t.Errorf("Callers count = %d, expected %d", len(result.Callers), len(tt.expected.Callers))
			}
			if len(result.Callees) != len(tt.expected.Callees) {
				t.Errorf("Callees count = %d, expected %d", len(result.Callees), len(tt.expected.Callees))
			}
		})
	}
}

func fieldInt32Ptr(i int) *int32 {
	v := int32(i)
	return &v
}
