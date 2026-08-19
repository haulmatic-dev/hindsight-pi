package core

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// mockBeadsClient is a mock implementation of the beads client for testing
type mockBeadsClient struct {
	mu            sync.RWMutex
	beads         map[string]types.Bead // key: "project/repo-id" or "project-id"
	listErr       error
	listErrByPath map[string]error // per-projectPath list error (overrides listErr for that path)
	getErr        error
	closeErr      error
	closeCalls    []mockCloseCall
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
	if err, ok := m.listErrByPath[projectPath]; ok {
		return nil, err
	}
	if m.listErr != nil {
		return nil, m.listErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	projectName := filepath.Base(projectPath)

	var result []types.Bead
	for _, bead := range m.beads {
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

	if projectName == "" {
		projectName = filepath.Base(projectPath)
	}

	var result []types.Bead
	for _, bead := range m.beads {
		if bead.Status != types.StatusOpen {
			continue
		}

		hasUnmetDependencies := false
		for _, depID := range bead.Dependencies {
			if dep, exists := m.beads[string(depID)]; exists {
				if dep.Status == types.StatusOpen {
					hasUnmetDependencies = true
					break
				}
			}
		}

		if hasUnmetDependencies {
			continue
		}

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

func (m *mockBeadsClient) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	projectName := filepath.Base(projectPath)
	beadID := types.BeadID(fmt.Sprintf("%s-fix-%d", projectName, len(m.beads)+1))

	bead := types.Bead{
		ID:           beadID,
		Title:        req.Title,
		Description:  req.Description,
		Type:         req.Type,
		Status:       types.StatusOpen,
		Priority:     req.Priority,
		Dependencies: req.Dependencies,
		Labels:       req.Labels,
		Project:      projectName,
	}
	m.beads[string(beadID)] = bead
	return beadID, nil
}

func (m *mockBeadsClient) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if bead, exists := m.beads[string(beadID)]; exists {
		bead.Labels = append(bead.Labels, labels...)
		m.beads[string(beadID)] = bead
	}
	return nil
}

func (m *mockBeadsClient) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if bead, exists := m.beads[string(beadID)]; exists {
		bead.Description = description
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
		return nil
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

	overseer.beadsClient = mockBeads

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
