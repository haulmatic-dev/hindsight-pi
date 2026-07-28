package graph

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/haulmatic-dev/hydra/graph/model"
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

// ============================================================================
// TEST RESOLVER INTERFACES
// ============================================================================

type fieldOverseerInterface interface {
	GetBeadDependencies(beadID string) ([]string, error)
	GetBeadDependents(beadID string) ([]string, error)
	GetBeadFromDAG(beadID string) (types.Bead, error)
	GetActiveWorkers() []types.Worker
}

// fieldTestBeadResolver wraps beadResolver with test-specific dependencies
type fieldTestBeadResolver struct {
	overseer fieldOverseerInterface
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
	// Patterns not implemented in test resolver - return empty
	return []*model.Pattern{}, nil
}

func (r *fieldTestBeadResolver) Impact(ctx context.Context, obj *model.Bead) (*model.ImpactAnalysis, error) {
	// Impact analysis was backed by llm-tldr, which has been removed. The
	// production resolver returns nil for the same reason.
	return nil, nil
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
	// Test that Patterns() returns empty slice (Hindsight integration removed cass_memory)
	resolver := &fieldTestBeadResolver{}

	bead := &model.Bead{
		ID:          "test-project-A",
		Title:       "Test Bead",
		Type:        "test",
		Project:     "test-project",
		Description: fieldStrPtr("Test description"),
	}

	ctx := context.Background()
	patterns, err := resolver.Patterns(ctx, bead)

	if err != nil {
		t.Fatalf("Patterns() unexpected error: %v", err)
	}

	if patterns == nil {
		t.Error("Patterns() returned nil, expected empty slice")
	}

	if len(patterns) != 0 {
		t.Errorf("Patterns() returned %d patterns, expected 0 (Hindsight integration removed cass_memory)", len(patterns))
	}
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

func fieldInt32Ptr(i int) *int32 {
	v := int32(i)
	return &v
}
