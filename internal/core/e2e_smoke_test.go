package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestE2E_BeadDiscoveryAndScheduling runs a smoke test with mock beads
// to verify the overseer can discover, build DAG, and identify ready beads.
func TestE2E_BeadDiscoveryAndScheduling(t *testing.T) {
	config := DefaultConfig()
	config.Overseer.MaxWorkers = 2
	config.Overseer.BeadDiscoveryInterval = 3 * time.Second
	config.Intelligence.Graphify.Enabled = false
	config.Intelligence.GPTCache.Enabled = false
	config.Intelligence.Hindsight.Enabled = false
	config.Intelligence.Patterns.Enabled = false
	config.Intelligence.SettledState.Enabled = false
	config.Intelligence.CollectiveUnconscious.Enabled = false
	config.MCP.Enabled = false
	config.API.Enabled = false
	config.Agents.Enabled = false

	overseer := NewOverseer(config)

	// Set up mock beads client
	mockBeads := newMockBeadsClientForE2E()
	overseer.beadsClient = mockBeads

	// Set up test project
	overseer.projects = []types.Project{
		{
			Name: "url-shortener",
			Path: "/tmp/hydra-e2e-test/url-shortener",
			Type: types.RepoTypeSingle,
		},
	}

	// Build DAG
	if err := overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG() failed: %v", err)
	}

	// Check DAG stats
	stats := overseer.GetDAGStats()
	t.Logf("DAG Stats: %+v", stats)

	if stats.TotalNodes != 4 {
		t.Errorf("Expected 4 nodes in DAG, got %d", stats.TotalNodes)
	}

	if stats.ReadyNodes != 1 {
		t.Errorf("Expected 1 ready node (the RED bead), got %d", stats.ReadyNodes)
	}

	if stats.BlockedNodes != 3 {
		t.Errorf("Expected 3 blocked nodes, got %d", stats.BlockedNodes)
	}

	// Get ready beads
	ready := overseer.GetReadyBeadsFromDAG()
	if len(ready) != 1 {
		t.Fatalf("Expected 1 ready bead, got %d", len(ready))
	}

	if ready[0].ID != "url-shortener-001" {
		t.Errorf("Expected url-shortener-001 to be ready, got %s", ready[0].ID)
	}

	if ready[0].Type != "test" {
		t.Errorf("Expected type 'test', got %s", ready[0].Type)
	}

	// Verify dependencies
	deps, err := overseer.GetBeadDependencies("url-shortener-002")
	if err != nil {
		t.Fatalf("GetBeadDependencies failed: %v", err)
	}
	if len(deps) != 1 || deps[0] != "url-shortener-001" {
		t.Errorf("Expected url-shortener-002 to depend on url-shortener-001, got %v", deps)
	}

	// Mark first bead as completed and verify next becomes ready
	_, err = overseer.dag.MarkCompleted("url-shortener-001")
	if err != nil {
		t.Fatalf("MarkCompleted failed: %v", err)
	}

	ready = overseer.GetReadyBeadsFromDAG()
	if len(ready) != 1 {
		t.Fatalf("Expected 1 ready bead after completion, got %d", len(ready))
	}

	if ready[0].ID != "url-shortener-002" {
		t.Errorf("Expected url-shortener-002 to be ready after 001 completed, got %s", ready[0].ID)
	}

	fmt.Println("✅ E2E smoke test passed")
}

type e2eMockBeadsClient struct {
	beads map[string]types.Bead
}

func newMockBeadsClientForE2E() *e2eMockBeadsClient {
	m := &e2eMockBeadsClient{beads: make(map[string]types.Bead)}
	beads := []types.Bead{
		{ID: "url-shortener-001", Title: "RED: Write tests", Type: "test", Status: types.StatusOpen, Project: "url-shortener", Priority: 10},
		{ID: "url-shortener-002", Title: "GREEN: Implement", Type: "implement", Status: types.StatusOpen, Project: "url-shortener", Priority: 10, Dependencies: []types.BeadID{"url-shortener-001"}},
		{ID: "url-shortener-003", Title: "REFACTOR: Clean up", Type: "refactor", Status: types.StatusOpen, Project: "url-shortener", Priority: 5, Dependencies: []types.BeadID{"url-shortener-002"}},
		{ID: "url-shortener-004", Title: "Add validation", Type: "implement", Status: types.StatusOpen, Project: "url-shortener", Priority: 3, Dependencies: []types.BeadID{"url-shortener-003"}},
	}
	for _, b := range beads {
		m.beads[string(b.ID)] = b
	}
	return m
}

func (m *e2eMockBeadsClient) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	var result []types.Bead
	for _, b := range m.beads {
		result = append(result, b)
	}
	return result, nil
}

func (m *e2eMockBeadsClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	if b, ok := m.beads[string(beadID)]; ok {
		return &b, nil
	}
	return nil, fmt.Errorf("bead not found: %s", beadID)
}

func (m *e2eMockBeadsClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	return nil
}

func (m *e2eMockBeadsClient) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	return m.ListBeads(projectPath, repoName)
}

func (m *e2eMockBeadsClient) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	if b, ok := m.beads[string(beadID)]; ok {
		b.Status = status
		m.beads[string(beadID)] = b
	}
	return nil
}

func (m *e2eMockBeadsClient) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	return "", nil
}

func (m *e2eMockBeadsClient) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}

func (m *e2eMockBeadsClient) RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}

func (m *e2eMockBeadsClient) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	return nil
}
