package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestUpdateConfig_AddsProject(t *testing.T) {
	config := DefaultConfig()
	config.MCP.Enabled = false

	overseer := NewOverseer(config)
	defer func() {
		if overseer.mcpManager != nil {
			_ = overseer.mcpManager.Stop()
		}
	}()

	initialProjects := []types.Project{
		{Name: "alpha", Path: "/test/alpha", Type: types.RepoTypeSingle},
	}
	overseer.projects = initialProjects

	mockBeads := newMockBeadsClient()
	overseer.beadsClient = mockBeads

	completed := make(map[string]bool)
	overseer.beadDiscovery = NewBeadDiscovery(mockBeads, nil, initialProjects, completed, nil, nil)
	overseer.beadAnalyzer = NewBeadAnalyzer(initialProjects)
	overseer.beadFileReader = NewBeadFileReader(initialProjects)
	overseer.projectResolver = NewProjectResolver(initialProjects)

	mockBeads.beads["beta-1"] = types.Bead{
		ID:       "beta-1",
		Project:  "beta",
		Type:     "implement",
		Status:   types.StatusOpen,
		Priority: 1,
	}

	newConfig := DefaultConfig()
	newConfig.MCP.Enabled = false
	newConfig.Projects = []ProjectConfig{
		{Name: "alpha", Path: "/test/alpha", Type: "single"},
		{Name: "beta", Path: "/test/beta", Type: "single"},
	}

	if err := overseer.configManager.UpdateConfig(newConfig); err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	if len(overseer.projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(overseer.projects))
	}

	ready, err := overseer.beadDiscovery.FindReadyBeads(map[string]bool{})
	if err != nil {
		t.Fatalf("FindReadyBeads failed: %v", err)
	}
	if len(ready) != 1 || string(ready[0].ID) != "beta-1" {
		t.Fatalf("expected 1 ready bead (beta-1), got %+v", ready)
	}
}

func TestUpdateConfig_RemovesProject(t *testing.T) {
	config := DefaultConfig()
	config.MCP.Enabled = false

	overseer := NewOverseer(config)
	defer func() {
		if overseer.mcpManager != nil {
			_ = overseer.mcpManager.Stop()
		}
	}()

	initialProjects := []types.Project{
		{Name: "alpha", Path: "/test/alpha", Type: types.RepoTypeSingle},
		{Name: "beta", Path: "/test/beta", Type: types.RepoTypeSingle},
	}
	overseer.projects = initialProjects

	mockBeads := newMockBeadsClient()
	overseer.beadsClient = mockBeads

	completed := make(map[string]bool)
	overseer.beadDiscovery = NewBeadDiscovery(mockBeads, nil, initialProjects, completed, nil, nil)
	overseer.beadAnalyzer = NewBeadAnalyzer(initialProjects)
	overseer.beadFileReader = NewBeadFileReader(initialProjects)
	overseer.projectResolver = NewProjectResolver(initialProjects)

	mockBeads.beads["alpha-1"] = types.Bead{
		ID:       "alpha-1",
		Project:  "alpha",
		Type:     "implement",
		Status:   types.StatusOpen,
		Priority: 1,
	}
	mockBeads.beads["beta-1"] = types.Bead{
		ID:       "beta-1",
		Project:  "beta",
		Type:     "implement",
		Status:   types.StatusOpen,
		Priority: 1,
	}

	overseer.activeWorkers["beta-1"] = &types.Worker{
		ID:          "beta-1",
		BeadID:      "beta-1",
		StartTime:   time.Now(),
		ProjectPath: "/test/beta",
		Status:      "running",
	}

	newConfig := DefaultConfig()
	newConfig.MCP.Enabled = false
	newConfig.Projects = []ProjectConfig{
		{Name: "alpha", Path: "/test/alpha", Type: "single"},
	}

	if err := overseer.configManager.UpdateConfig(newConfig); err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	if len(overseer.projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(overseer.projects))
	}
	if overseer.projects[0].Name != "alpha" {
		t.Fatalf("expected remaining project alpha, got %s", overseer.projects[0].Name)
	}

	ready, err := overseer.beadDiscovery.FindReadyBeads(map[string]bool{})
	if err != nil {
		t.Fatalf("FindReadyBeads failed: %v", err)
	}
	if len(ready) != 1 || string(ready[0].ID) != "alpha-1" {
		t.Fatalf("expected 1 ready bead (alpha-1), got %+v", ready)
	}

	if _, ok := overseer.activeWorkers["beta-1"]; !ok {
		t.Fatalf("in-flight worker for removed project beta was unexpectedly cleared")
	}
}
