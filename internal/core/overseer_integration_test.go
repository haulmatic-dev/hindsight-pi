package core

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// mockBeadsClient is a mock implementation of the beads client for testing
type mockBeadsClient struct {
	mu         sync.RWMutex
	beads      map[string]types.Bead // key: "project/repo-id" or "project-id"
	listErr    error
	getErr     error
	closeErr   error
	closeCalls []mockCloseCall
}

// mockCloseCall tracks CloseBead invocations for testing
type mockCloseCall struct {
	projectPath string
	repoName    string
	beadID      types.BeadID
	reason      string
}

func newMockBeadsClient() *mockBeadsClient {
	return &mockBeadsClient{
		beads:      make(map[string]types.Bead),
		closeCalls: make([]mockCloseCall, 0),
	}
}

func (m *mockBeadsClient) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Extract project name from path (basename)
	projectName := filepath.Base(projectPath)

	var result []types.Bead
	for _, bead := range m.beads {
		// Filter by project/repo
		if repoName != "" {
			if bead.Project == projectName && bead.Repo == repoName {
				result = append(result, bead)
			}
		} else {
			if bead.Project == projectName {
				result = append(result, bead)
			}
		}
	}
	return result, nil
}

func (m *mockBeadsClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if bead, exists := m.beads[string(beadID)]; exists {
		return &bead, nil
	}
	return nil, nil
}

func (m *mockBeadsClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	if m.closeErr != nil {
		return m.closeErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.closeCalls = append(m.closeCalls, mockCloseCall{
		projectPath: projectPath,
		repoName:    repoName,
		beadID:      beadID,
		reason:      reason,
	})

	// Update bead status
	if bead, exists := m.beads[string(beadID)]; exists {
		if reason == "completed" {
			bead.Status = types.StatusCompleted
		} else if reason == "failed" {
			bead.Status = types.StatusFailed
		}
		now := time.Now()
		bead.CompletedAt = &now
		m.beads[string(beadID)] = bead
	}

	return nil
}

func (m *mockBeadsClient) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Use the provided projectName, but fallback to extracting from path if empty
	if projectName == "" {
		projectName = filepath.Base(projectPath)
	}

	var result []types.Bead
	for _, bead := range m.beads {
		// Only consider open beads
		if bead.Status != types.StatusOpen {
			continue
		}

		// Check if bead has any dependencies
		// A "ready" bead should have no unresolved dependencies
		hasUnmetDependencies := false
		for _, depID := range bead.Dependencies {
			if dep, exists := m.beads[string(depID)]; exists {
				// Check if dependency is completed (not open)
				if dep.Status == types.StatusOpen {
					hasUnmetDependencies = true
					break
				}
			}
		}

		// Skip beads with unmet dependencies
		if hasUnmetDependencies {
			continue
		}

		// Filter by project/repo
		if repoName != "" {
			if bead.Project == projectName && bead.Repo == repoName {
				result = append(result, bead)
			}
		} else {
			if bead.Project == projectName {
				result = append(result, bead)
			}
		}
	}
	return result, nil
}

func (m *mockBeadsClient) addBead(bead types.Bead) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beads[string(bead.ID)] = bead
}

func (m *mockBeadsClient) getCloseCalls() []mockCloseCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]mockCloseCall{}, m.closeCalls...)
}

func (m *mockBeadsClient) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if bead, exists := m.beads[string(beadID)]; exists {
		bead.Status = status
		m.beads[string(beadID)] = bead
	}
	return nil
}

// mockWorkerSpawner tracks worker spawn calls for testing
type mockWorkerSpawner struct {
	mu          sync.RWMutex
	spawned     []types.Bead
	spawnErr    error
	maxWorkers  int
	activeCount int
}

func newMockWorkerSpawner() *mockWorkerSpawner {
	return &mockWorkerSpawner{
		spawned:    make([]types.Bead, 0),
		maxWorkers: 10,
	}
}

func (m *mockWorkerSpawner) spawnWorker(bead types.Bead) error {
	if m.spawnErr != nil {
		return m.spawnErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeCount >= m.maxWorkers {
		return nil // Silently skip if at max
	}

	m.spawned = append(m.spawned, bead)
	m.activeCount++
	return nil
}

func (m *mockWorkerSpawner) getSpawned() []types.Bead {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]types.Bead{}, m.spawned...)
}

func (m *mockWorkerSpawner) clearSpawned() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawned = make([]types.Bead, 0)
}

func (m *mockWorkerSpawner) setActiveCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeCount = count
}

// testOverseerWithMocks creates an overseer with mock dependencies for testing
type testOverseerWithMocks struct {
	overseer     *Overseer
	mockBeads    *mockBeadsClient
	mockSpawner  *mockWorkerSpawner
	testProjects []types.Project
}

func setupTestOverseer(t *testing.T) *testOverseerWithMocks {
	t.Helper()

	config := DefaultConfig()
	config.Overseer.MaxWorkers = 4
	config.Overseer.BeadDiscoveryInterval = 100 * time.Millisecond

	overseer := NewOverseer(config)

	mockBeads := newMockBeadsClient()
	mockSpawner := newMockWorkerSpawner()

	// Replace the beads client with mock
	overseer.beadsClient = mockBeads

	// Set up test projects (Path basename must match bead.Project)
	testProjects := []types.Project{
		{
			Name: "test-project",
			Path: "/test/test-project",
			Type: types.RepoTypeSingle,
		},
	}
	overseer.projects = testProjects

	return &testOverseerWithMocks{
		overseer:     overseer,
		mockBeads:    mockBeads,
		mockSpawner:  mockSpawner,
		testProjects: testProjects,
	}
}

// TestOverseer_BuildDAG tests that the DAG is built correctly from beads
func TestOverseer_BuildDAG(t *testing.T) {
	tests := []struct {
		name        string
		beads       []types.Bead
		wantNodes   int
		wantEdges   int
		wantBlocked int
	}{
		{
			name: "simple beads without dependencies",
			beads: []types.Bead{
				{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
				{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project"},
				{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project"},
			},
			wantNodes:   3,
			wantEdges:   0,
			wantBlocked: 0,
		},
		{
			name: "linear dependency chain",
			beads: []types.Bead{
				{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
				{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
				{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-B"}},
			},
			wantNodes:   3,
			wantEdges:   2,
			wantBlocked: 2, // B and C are blocked
		},
		{
			name: "diamond dependency pattern",
			beads: []types.Bead{
				{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
				{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
				{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
				{ID: "test-project-D", Title: "Bead D", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-B", "test-project-C"}},
			},
			wantNodes:   4,
			wantEdges:   4,
			wantBlocked: 3, // B, C, and D are blocked
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := setupTestOverseer(t)

			// Add beads to mock client
			for _, bead := range tt.beads {
				test.mockBeads.addBead(bead)
			}

			// Build DAG
			err := test.overseer.buildDAG()
			if err != nil {
				t.Fatalf("buildDAG() error = %v", err)
			}

			// Verify node count
			if got := test.overseer.dag.GetNodeCount(); got != tt.wantNodes {
				t.Errorf("GetNodeCount() = %d, want %d", got, tt.wantNodes)
			}

			// Verify blocked count
			if got := test.overseer.dag.GetBlockedCount(); got != tt.wantBlocked {
				t.Errorf("GetBlockedCount() = %d, want %d", got, tt.wantBlocked)
			}

			// Verify all expected nodes exist
			for _, bead := range tt.beads {
				if !test.overseer.dag.HasNode(string(bead.ID)) {
					t.Errorf("Node %s not found in DAG", bead.ID)
				}
			}
		})
	}
}

// TestOverseer_GetReadyBeadsFromDAG tests the dependency resolution flow
func TestOverseer_GetReadyBeadsFromDAG(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads with dependencies: A <- B <- C
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project", Type: "implement"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Type: "implement", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Type: "implement", Dependencies: []types.BeadID{"test-project-B"}},
	}

	// Add beads to mock client
	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Build DAG
	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Step 1: Initially, only A should be ready (InDegree 0)
	ready := test.overseer.GetReadyBeadsFromDAG()
	if len(ready) != 1 || ready[0].ID != "test-project-A" {
		t.Errorf("Expected only bead A to be ready, got: %v", ready)
	}

	// Step 2: Mark A as completed
	_, err := test.overseer.dag.MarkCompleted("test-project-A")
	if err != nil {
		t.Fatalf("MarkCompleted(A) error = %v", err)
	}

	// Step 3: B should now be ready
	ready = test.overseer.GetReadyBeadsFromDAG()
	if err != nil {
		t.Fatalf("getReadyBeadsFromDAG() error = %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "test-project-B" {
		t.Errorf("Expected only bead B to be ready after A completed, got: %v", ready)
	}

	// Step 4: Mark B as completed
	_, err = test.overseer.dag.MarkCompleted("test-project-B")
	if err != nil {
		t.Fatalf("MarkCompleted(B) error = %v", err)
	}

	// Step 5: C should now be ready
	ready = test.overseer.GetReadyBeadsFromDAG()
	if err != nil {
		t.Fatalf("getReadyBeadsFromDAG() error = %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "test-project-C" {
		t.Errorf("Expected only bead C to be ready after B completed, got: %v", ready)
	}

	// Step 6: Mark C as completed
	_, err = test.overseer.dag.MarkCompleted("test-project-C")
	if err != nil {
		t.Fatalf("MarkCompleted(C) error = %v", err)
	}

	// Step 7: No beads should be ready now
	ready = test.overseer.GetReadyBeadsFromDAG()
	if err != nil {
		t.Fatalf("getReadyBeadsFromDAG() error = %v", err)
	}
	if len(ready) != 0 {
		t.Errorf("Expected no beads to be ready after all completed, got: %v", ready)
	}
}

// TestOverseer_PriorityOrdering tests that higher priority beads are selected first
func TestOverseer_PriorityOrdering(t *testing.T) {
	test := setupTestOverseer(t)

	// Create multiple ready beads with different priorities
	beads := []types.Bead{
		{ID: "test-project-low", Title: "Low Priority", Status: types.StatusOpen, Project: "test-project", Type: "implement", Priority: 1},
		{ID: "test-project-med", Title: "Medium Priority", Status: types.StatusOpen, Project: "test-project", Type: "implement", Priority: 5},
		{ID: "test-project-high", Title: "High Priority", Status: types.StatusOpen, Project: "test-project", Type: "implement", Priority: 10},
		{ID: "test-project-crit", Title: "Critical Priority", Status: types.StatusOpen, Project: "test-project", Type: "implement", Priority: 20},
	}

	// Add beads to mock client
	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Build DAG
	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Get ready beads
	ready := test.overseer.GetReadyBeadsFromDAG()

	if len(ready) != 4 {
		t.Fatalf("Expected 4 ready beads, got %d", len(ready))
	}

	// Verify ordering: highest priority first
	expectedOrder := []types.BeadID{"test-project-crit", "test-project-high", "test-project-med", "test-project-low"}
	for i, expectedID := range expectedOrder {
		if ready[i].ID != expectedID {
			t.Errorf("Position %d: expected %s, got %s (priority %d)",
				i, expectedID, ready[i].ID, ready[i].Priority)
		}
	}

	// Verify priorities are in descending order
	for i := 1; i < len(ready); i++ {
		if ready[i].Priority > ready[i-1].Priority {
			t.Errorf("Priority ordering failed at position %d: %d > %d",
				i, ready[i].Priority, ready[i-1].Priority)
		}
	}
}

// TestOverseer_CycleDetection tests that cycles are properly detected and handled
func TestOverseer_CycleDetection(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads with cycle: A <- B <- C <- A
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-B"}},
	}

	// Add beads to mock client
	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Build DAG first
	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Verify no cycle initially
	cycle, err := test.overseer.dag.DetectCycle()
	if err != nil {
		t.Fatalf("DetectCycle() error = %v", err)
	}
	if cycle != nil {
		t.Errorf("Expected no cycle initially, got: %v", cycle)
	}

	// Now manually create the cycle using internal method
	test.overseer.dag.addEdgeInternal("test-project-A", "test-project-C")

	// Verify cycle is detected
	cycle, err = test.overseer.dag.DetectCycle()
	if err == nil {
		t.Error("Expected error for cycle detection, got nil")
	}
	if cycle == nil {
		t.Error("Expected cycle to be detected, got nil")
	}

	// Verify cycle contains expected nodes
	cycleFound := false
	for _, node := range cycle {
		if node == "test-project-A" || node == "test-project-B" || node == "test-project-C" {
			cycleFound = true
			break
		}
	}
	if !cycleFound {
		t.Errorf("Cycle doesn't contain expected nodes: %v", cycle)
	}
}

// TestOverseer_WorkerSpawning tests worker spawning logic
func TestOverseer_WorkerSpawning(t *testing.T) {
	tests := []struct {
		name          string
		readyBeads    int
		maxWorkers    int
		activeWorkers int
		expectedSpawn int
	}{
		{
			name:          "spawn all when slots available",
			readyBeads:    3,
			maxWorkers:    5,
			activeWorkers: 0,
			expectedSpawn: 3,
		},
		{
			name:          "respect max workers limit",
			readyBeads:    10,
			maxWorkers:    4,
			activeWorkers: 2,
			expectedSpawn: 2, // Only 2 slots available (4 - 2)
		},
		{
			name:          "no spawn when at max workers",
			readyBeads:    5,
			maxWorkers:    3,
			activeWorkers: 3,
			expectedSpawn: 0,
		},
		{
			name:          "spawn limited by both beads and slots",
			readyBeads:    2,
			maxWorkers:    5,
			activeWorkers: 4,
			expectedSpawn: 1, // Only 1 slot available
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := setupTestOverseer(t)
			test.overseer.SetMaxWorkers(tt.maxWorkers)

			// Create ready beads
			for i := 0; i < tt.readyBeads; i++ {
				beadID := types.NewNumericBeadID("test-project", "", i)
				test.mockBeads.addBead(types.Bead{
					ID:       beadID,
					Title:    "Bead " + beadID.ID(),
					Status:   types.StatusOpen,
					Project:  "test-project",
					Type:     "implement",
					Priority: i,
				})
			}

			// Build DAG
			if err := test.overseer.buildDAG(); err != nil {
				t.Fatalf("buildDAG() error = %v", err)
			}

			// Simulate active workers
			test.mockSpawner.setActiveCount(tt.activeWorkers)

			// Get ready beads
			ready := test.overseer.GetReadyBeadsFromDAG()

			// Calculate available slots
			availableSlots := tt.maxWorkers - tt.activeWorkers
			if availableSlots < 0 {
				availableSlots = 0
			}

			// Simulate spawning
			spawned := 0
			for _, bead := range ready {
				if availableSlots <= 0 {
					break
				}
				if err := test.mockSpawner.spawnWorker(bead); err != nil {
					t.Errorf("spawnWorker() error = %v", err)
					continue
				}
				spawned++
				availableSlots--
			}

			// Verify expected spawn count
			if spawned != tt.expectedSpawn {
				t.Errorf("Expected %d workers spawned, got %d", tt.expectedSpawn, spawned)
			}

			// Verify beads removed from ready pool (mock verification)
			spawnedBeads := test.mockSpawner.getSpawned()
			if len(spawnedBeads) != tt.expectedSpawn {
				t.Errorf("Expected %d spawned beads in mock, got %d", tt.expectedSpawn, len(spawnedBeads))
			}
		})
	}
}

// TestOverseer_WorkerCompletion tests worker completion handling
func TestOverseer_WorkerCompletion(t *testing.T) {
	tests := []struct {
		name               string
		beads              []types.Bead
		completeBead       string
		expectSuccess      bool
		expectNewlyReady   []string
		expectStillBlocked []string
	}{
		{
			name: "success unblocks dependents",
			beads: []types.Bead{
				{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
				{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
				{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
			},
			completeBead:       "test-project-A",
			expectSuccess:      true,
			expectNewlyReady:   []string{"test-project-B", "test-project-C"},
			expectStillBlocked: []string{},
		},
		{
			name: "failure keeps dependents blocked",
			beads: []types.Bead{
				{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
				{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
			},
			completeBead:       "test-project-A",
			expectSuccess:      false,
			expectNewlyReady:   []string{},
			expectStillBlocked: []string{"test-project-B"},
		},
		{
			name: "chain unblocking on success",
			beads: []types.Bead{
				{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
				{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
				{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-B"}},
			},
			completeBead:       "test-project-A",
			expectSuccess:      true,
			expectNewlyReady:   []string{"test-project-B"}, // Only B, C still blocked by B
			expectStillBlocked: []string{"test-project-C"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := setupTestOverseer(t)

			// Add beads to mock client
			for _, bead := range tt.beads {
				test.mockBeads.addBead(bead)
			}

			// Build DAG
			if err := test.overseer.buildDAG(); err != nil {
				t.Fatalf("buildDAG() error = %v", err)
			}

			if tt.expectSuccess {
				// Test successful completion
				newlyReady, err := test.overseer.dag.MarkCompleted(tt.completeBead)
				if err != nil {
					t.Fatalf("MarkCompleted() error = %v", err)
				}

				// Verify newly ready beads
				if len(newlyReady) != len(tt.expectNewlyReady) {
					t.Errorf("Expected %d newly ready beads, got %d: %v",
						len(tt.expectNewlyReady), len(newlyReady), newlyReady)
				}
				for _, expected := range tt.expectNewlyReady {
					found := false
					for _, actual := range newlyReady {
						if actual == expected {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected %s to be newly ready, but wasn't in %v", expected, newlyReady)
					}
				}

				// Verify still blocked beads
				for _, blockedID := range tt.expectStillBlocked {
					node, exists := test.overseer.dag.GetNode(blockedID)
					if !exists {
						t.Errorf("Blocked bead %s not found in DAG", blockedID)
						continue
					}
					if node.InDegree == 0 {
						t.Errorf("Expected %s to still be blocked, but InDegree is 0", blockedID)
					}
				}

				// Verify completed status
				node, _ := test.overseer.dag.GetNode(tt.completeBead)
				if node.Status() != types.StatusCompleted {
					t.Errorf("Expected %s status to be completed, got %v", tt.completeBead, node.Status())
				}
			} else {
				// Test failure
				err := test.overseer.dag.MarkFailed(tt.completeBead)
				if err != nil {
					t.Fatalf("MarkFailed() error = %v", err)
				}

				// Verify failed status
				node, _ := test.overseer.dag.GetNode(tt.completeBead)
				if node.Status() != types.StatusFailed {
					t.Errorf("Expected %s status to be failed, got %v", tt.completeBead, node.Status())
				}

				// Verify dependents are still blocked
				for _, blockedID := range tt.expectStillBlocked {
					blockedNode, exists := test.overseer.dag.GetNode(blockedID)
					if !exists {
						t.Errorf("Blocked bead %s not found in DAG", blockedID)
						continue
					}
					if blockedNode.InDegree == 0 {
						t.Errorf("Expected %s to still be blocked after failure, but InDegree is 0", blockedID)
					}
				}

				// Get ready beads - should not include blocked ones
				ready := test.overseer.GetReadyBeadsFromDAG()
				if err != nil {
					t.Fatalf("getReadyBeadsFromDAG() error = %v", err)
				}
				for _, bead := range ready {
					for _, blockedID := range tt.expectStillBlocked {
						if string(bead.ID) == blockedID {
							t.Errorf("Expected %s to be blocked, but found in ready list", blockedID)
						}
					}
				}
			}
		})
	}
}

// TestOverseer_DAGStats tests DAG statistics
func TestOverseer_DAGStats(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads with various states
	beads := []types.Bead{
		{ID: "test-project-A", Title: "Bead A", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-B", Title: "Bead B", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-C", Title: "Bead C", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-A"}},
		{ID: "test-project-D", Title: "Bead D", Status: types.StatusOpen, Project: "test-project", Dependencies: []types.BeadID{"test-project-B", "test-project-C"}},
	}

	// Add beads to mock client
	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Build DAG
	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Get initial stats
	stats := test.overseer.GetDAGStats()

	// Verify initial stats
	if stats.TotalNodes != 4 {
		t.Errorf("Expected 4 total nodes, got %d", stats.TotalNodes)
	}
	if stats.ReadyNodes != 1 { // Only A is ready
		t.Errorf("Expected 1 ready node, got %d", stats.ReadyNodes)
	}
	if stats.BlockedNodes != 3 { // B, C, D are blocked
		t.Errorf("Expected 3 blocked nodes, got %d", stats.BlockedNodes)
	}
	if stats.CompletedNodes != 0 {
		t.Errorf("Expected 0 completed nodes, got %d", stats.CompletedNodes)
	}

	// Complete A
	test.overseer.dag.MarkCompleted("test-project-A")

	// Get updated stats
	stats = test.overseer.GetDAGStats()

	if stats.CompletedNodes != 1 {
		t.Errorf("Expected 1 completed node, got %d", stats.CompletedNodes)
	}
	if stats.ReadyNodes != 2 { // B and C are now ready
		t.Errorf("Expected 2 ready nodes, got %d", stats.ReadyNodes)
	}
	if stats.BlockedNodes != 1 { // Only D is blocked
		t.Errorf("Expected 1 blocked node, got %d", stats.BlockedNodes)
	}
}

// TestOverseer_MultiProjectDAG tests DAG building across multiple projects
func TestOverseer_MultiProjectDAG(t *testing.T) {
	config := DefaultConfig()
	overseer := NewOverseer(config)

	mockBeads := newMockBeadsClient()
	overseer.beadsClient = mockBeads

	// Set up multiple test projects (Path basename must match bead.Project)
	overseer.projects = []types.Project{
		{
			Name: "project-alpha",
			Path: "/test/project-alpha",
			Type: types.RepoTypeSingle,
		},
		{
			Name: "project-beta",
			Path: "/test/project-beta",
			Type: types.RepoTypeMulti,
			Repos: []types.Repo{
				{Name: "backend", Path: "backend"},
				{Name: "frontend", Path: "frontend"},
			},
		},
	}

	// Add beads from different projects
	beads := []types.Bead{
		// Project Alpha (single repo)
		{ID: "project-alpha-A", Title: "Alpha A", Status: types.StatusOpen, Project: "project-alpha"},
		{ID: "project-alpha-B", Title: "Alpha B", Status: types.StatusOpen, Project: "project-alpha", Dependencies: []types.BeadID{"project-alpha-A"}},

		// Project Beta - Backend repo
		{ID: "project-beta/backend-C", Title: "Beta Backend C", Status: types.StatusOpen, Project: "project-beta", Repo: "backend"},

		// Project Beta - Frontend repo
		{ID: "project-beta/frontend-D", Title: "Beta Frontend D", Status: types.StatusOpen, Project: "project-beta", Repo: "frontend"},
	}

	for _, bead := range beads {
		mockBeads.addBead(bead)
	}

	// Build DAG
	if err := overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Verify all beads from all projects are in DAG
	if overseer.dag.GetNodeCount() != 4 {
		t.Errorf("Expected 4 total nodes across all projects, got %d", overseer.dag.GetNodeCount())
	}

	// Verify project-alpha beads
	if !overseer.dag.HasNode("project-alpha-A") {
		t.Error("project-alpha-A not found in DAG")
	}
	if !overseer.dag.HasNode("project-alpha-B") {
		t.Error("project-alpha-B not found in DAG")
	}

	// Verify project-beta beads
	if !overseer.dag.HasNode("project-beta/backend-C") {
		t.Error("project-beta/backend-C not found in DAG")
	}
	if !overseer.dag.HasNode("project-beta/frontend-D") {
		t.Error("project-beta/frontend-D not found in DAG")
	}

	// Verify dependency resolution works across projects
	ready := overseer.GetReadyBeadsFromDAG()
	if len(ready) != 3 { // A, C, D are ready (B depends on A)
		t.Errorf("Expected 3 ready beads, got %d", len(ready))
	}
}

// TestOverseer_ApprovalGateFiltering tests that approval gates are filtered from ready beads
func TestOverseer_ApprovalGateFiltering(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads including approval gates
	beads := []types.Bead{
		{ID: "test-project-normal", Title: "Normal Bead", Status: types.StatusOpen, Project: "test-project", Type: "implement"},
		{ID: "test-project-approval", Title: "Approval Gate", Status: types.StatusOpen, Project: "test-project", Type: "approval-gate"},
		{ID: "test-project-test", Title: "Test Bead", Status: types.StatusOpen, Project: "test-project", Type: "test"},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Build DAG
	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Get ready beads
	ready := test.overseer.GetReadyBeadsFromDAG()

	// Verify approval gate is filtered out
	if len(ready) != 2 {
		t.Errorf("Expected 2 ready beads (excluding approval gate), got %d", len(ready))
	}

	for _, bead := range ready {
		if bead.Type == "approval-gate" {
			t.Errorf("Approval gate %s should be filtered from ready beads", bead.ID)
		}
	}
}

// TestOverseer_ConcurrentAccess tests thread safety of overseer operations
func TestOverseer_ConcurrentAccess(t *testing.T) {
	test := setupTestOverseer(t)

	// Create multiple beads
	for i := 0; i < 10; i++ {
		beadID := types.NewNumericBeadID("test-project", "", i)
		test.mockBeads.addBead(types.Bead{
			ID:      beadID,
			Title:   "Bead " + beadID.ID(),
			Status:  types.StatusOpen,
			Project: "test-project",
			Type:    "implement",
		})
	}

	// Build DAG
	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Concurrent operations
	var wg sync.WaitGroup
	errors := make(chan error, 20)

	// Concurrent reads
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			test.overseer.GetReadyBeadsFromDAG()
		}()
	}

	// Concurrent stats reads
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = test.overseer.GetDAGStats()
		}()
	}

	// Wait for all goroutines
	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}
}

// TestOverseer_CompletedBeadsFiltering tests that completed beads are filtered
func TestOverseer_CompletedBeadsFiltering(t *testing.T) {
	test := setupTestOverseer(t)

	// Create beads with different statuses
	beads := []types.Bead{
		{ID: "test-project-open", Title: "Open Bead", Status: types.StatusOpen, Project: "test-project"},
		{ID: "test-project-progress", Title: "In Progress Bead", Status: types.StatusInProgress, Project: "test-project"},
		{ID: "test-project-completed", Title: "Completed Bead", Status: types.StatusCompleted, Project: "test-project"},
		{ID: "test-project-failed", Title: "Failed Bead", Status: types.StatusFailed, Project: "test-project"},
	}

	for _, bead := range beads {
		test.mockBeads.addBead(bead)
	}

	// Build DAG
	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Get ready beads
	ready := test.overseer.GetReadyBeadsFromDAG()

	// Only open beads should be ready
	if len(ready) != 1 {
		t.Errorf("Expected 1 ready bead (only open), got %d", len(ready))
	}

	if len(ready) > 0 && ready[0].ID != "test-project-open" {
		t.Errorf("Expected open bead to be ready, got %s", ready[0].ID)
	}
}

// TestOverseer_SessionCompletedTracking tests that beads completed in this session are tracked
func TestOverseer_SessionCompletedTracking(t *testing.T) {
	test := setupTestOverseer(t)

	// Create a bead
	bead := types.Bead{
		ID:      "test-project-track",
		Title:   "Tracking Test Bead",
		Status:  types.StatusOpen,
		Project: "test-project",
		Type:    "implement",
	}
	test.mockBeads.addBead(bead)

	// Build DAG
	if err := test.overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() error = %v", err)
	}

	// Verify bead is ready initially
	ready := test.overseer.GetReadyBeadsFromDAG()
	if len(ready) != 1 {
		t.Fatalf("Expected 1 ready bead initially, got %d", len(ready))
	}

	// Mark as completed in session tracking
	test.overseer.completed["test-project-track"] = true

	// Verify bead is no longer in ready list
	ready = test.overseer.GetReadyBeadsFromDAG()
	for _, b := range ready {
		if b.ID == "test-project-track" {
			t.Error("Completed bead should not appear in ready list")
		}
	}
}

// ============================================================================
// US-002 Overseer Integration Tests: Daily Skill Updates
// ============================================================================
// These tests verify that the overseer properly initializes and manages
// the skill updater for US-002 Daily Skill Updates feature.

// TestUS002_Overseer_InitializeSkillUpdater_AutoEnabled verifies that the
// overseer initializes the skill updater when auto-update is enabled
func TestUS002_Overseer_InitializeSkillUpdater_AutoEnabled(t *testing.T) {
	// Create config with auto-update enabled
	config := &Config{
		Pi: PiRootConfig{
			ConfigDir: "./test-pi-config",
			Skills: PiSkillsConfig{
				AutoUpdate:     true,
				UpdateInterval: "24h",
			},
		},
		Overseer: OverseerConfig{
			MaxWorkers:            4,
			BeadDiscoveryInterval: 1 * time.Second,
		},
	}

	// Create overseer with skill updater enabled
	overseer := NewOverseer(config)

	// Verify skill updater was initialized
	if overseer.skillUpdater == nil {
		t.Fatal("Skill updater should be initialized when AutoUpdate is enabled")
	}

	// Verify interval was set correctly
	if overseer.skillUpdater.GetUpdateInterval() != 24*time.Hour {
		t.Errorf("Skill updater interval = %v, want %v", overseer.skillUpdater.GetUpdateInterval(), 24*time.Hour)
	}
}

// TestUS002_Overseer_NoSkillUpdater_WhenAutoDisabled verifies that the
// overseer does NOT initialize skill updater when auto-update is disabled
func TestUS002_Overseer_NoSkillUpdater_WhenAutoDisabled(t *testing.T) {
	config := &Config{
		Pi: PiRootConfig{
			ConfigDir: "./test-pi-config",
			Skills: PiSkillsConfig{
				AutoUpdate:     false, // Disabled
				UpdateInterval: "24h",
			},
		},
		Overseer: OverseerConfig{
			MaxWorkers:            4,
			BeadDiscoveryInterval: 1 * time.Second,
		},
	}

	// Create overseer with skill updater disabled
	overseer := NewOverseer(config)

	// Verify skill updater was NOT initialized
	if overseer.skillUpdater != nil {
		t.Error("Skill updater should NOT be initialized when AutoUpdate is disabled")
	}
}

// TestUS002_Overseer_DefaultInterval verifies that a default interval
// of 24h is used when update_interval is not specified
func TestUS002_Overseer_DefaultInterval(t *testing.T) {
	config := &Config{
		Pi: PiRootConfig{
			ConfigDir: "./test-pi-config",
			Skills: PiSkillsConfig{
				AutoUpdate:     true,
				UpdateInterval: "", // Empty - should default to 24h
			},
		},
		Overseer: OverseerConfig{
			MaxWorkers:            4,
			BeadDiscoveryInterval: 1 * time.Second,
		},
	}

	// Create overseer
	overseer := NewOverseer(config)

	// Verify skill updater uses default interval
	if overseer.skillUpdater == nil {
		t.Fatal("Skill updater should be initialized")
	}

	if overseer.skillUpdater.GetUpdateInterval() != 24*time.Hour {
		t.Errorf("Default interval = %v, want %v", overseer.skillUpdater.GetUpdateInterval(), 24*time.Hour)
	}
}

// TestUS002_Overseer_CustomInterval verifies that custom intervals are properly set
func TestUS002_Overseer_CustomInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		want     time.Duration
	}{
		{"1h", "1h", 1 * time.Hour},
		{"30m", "30m", 30 * time.Minute},
		{"12h", "12h", 12 * time.Hour},
		{"2h30m", "2h30m", 2*time.Hour + 30*time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Pi: PiRootConfig{
					ConfigDir: "./test-pi-config",
					Skills: PiSkillsConfig{
						AutoUpdate:     true,
						UpdateInterval: tt.interval,
					},
				},
				Overseer: OverseerConfig{
					MaxWorkers:            4,
					BeadDiscoveryInterval: 1 * time.Second,
				},
			}

			overseer := NewOverseer(config)

			if overseer.skillUpdater == nil {
				t.Fatal("Skill updater should be initialized")
			}

			if overseer.skillUpdater.GetUpdateInterval() != tt.want {
				t.Errorf("Interval = %v, want %v", overseer.skillUpdater.GetUpdateInterval(), tt.want)
			}
		})
	}
}
