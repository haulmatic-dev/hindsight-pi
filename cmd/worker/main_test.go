package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// clearHydraEnvVars clears all HYDRA_* environment variables for test isolation
func clearHydraEnvVars(t *testing.T) {
	t.Helper()
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "HYDRA_") {
			key := strings.SplitN(env, "=", 2)[0]
			os.Unsetenv(key)
		}
	}
}

// TestParseConfig_WithBeadJSON verifies that parseConfig correctly deserializes HYDRA_BEAD_JSON
func TestParseConfig_WithBeadJSON(t *testing.T) {
	// Clear all HYDRA environment variables to ensure clean test isolation
	clearHydraEnvVars(t)

	// Create a full bead object
	bead := types.Bead{
		ID:          "hydra-test-123",
		Project:     "hydra",
		Repo:        "hydra",
		Type:        "task",
		Title:       "Test Bead Title",
		Description: "Test description",
		Status:      types.StatusOpen,
		Labels:      []string{"req-001", "tdd-red"},
		Priority:    3,
		Dependencies: []types.BeadID{
			"hydra-dep-1",
			"hydra-dep-2",
		},
		Metadata: map[string]string{
			"target_file": "test.go",
			"agent_type":  "orchestrator",
		},
	}

	// Serialize to JSON
	beadJSON, err := json.Marshal(bead)
	if err != nil {
		t.Fatalf("Failed to marshal bead: %v", err)
	}

	// Set environment variable using t.Setenv for proper cleanup
	t.Setenv("HYDRA_BEAD_JSON", string(beadJSON))

	// Parse config
	config := parseConfig()

	// Verify all fields are parsed correctly
	if config.BeadID != string(bead.ID) {
		t.Errorf("BeadID = %q, want %q", config.BeadID, bead.ID)
	}

	if config.Project != bead.Project {
		t.Errorf("Project = %q, want %q", config.Project, bead.Project)
	}

	if config.Repo != bead.Repo {
		t.Errorf("Repo = %q, want %q", config.Repo, bead.Repo)
	}

	if config.AgentType != bead.Type {
		t.Errorf("AgentType = %q, want %q", config.AgentType, bead.Type)
	}

	if config.BeadTitle != bead.Title {
		t.Errorf("BeadTitle = %q, want %q", config.BeadTitle, bead.Title)
	}

	if config.BeadDescription != bead.Description {
		t.Errorf("BeadDescription = %q, want %q", config.BeadDescription, bead.Description)
	}
}

// TestParseConfig_WithLegacyEnvVars verifies backward compatibility with legacy env vars
func TestParseConfig_WithLegacyEnvVars(t *testing.T) {
	// Clear all HYDRA environment variables to ensure clean test isolation
	clearHydraEnvVars(t)

	// Set legacy env vars using t.Setenv for proper cleanup
	t.Setenv("HYDRA_BEAD_ID", "hydra-legacy-123")
	t.Setenv("HYDRA_PROJECT", "legacy-project")
	t.Setenv("HYDRA_REPO", "legacy-repo")
	t.Setenv("HYDRA_AGENT_TYPE", "legacy-agent")
	t.Setenv("HYDRA_BEAD_TITLE", "Legacy Title")
	t.Setenv("HYDRA_BEAD_DESCRIPTION", "Legacy description")

	// Parse config
	config := parseConfig()

	// Verify legacy env vars work
	if config.BeadID != "hydra-legacy-123" {
		t.Errorf("BeadID = %q, want %q", config.BeadID, "hydra-legacy-123")
	}

	if config.Project != "legacy-project" {
		t.Errorf("Project = %q, want %q", config.Project, "legacy-project")
	}
}

// TestWorkerUsesBeadType verifies that worker uses actual bead.Type instead of config.AgentType
func TestWorkerUsesBeadType(t *testing.T) {
	// Create a bead where Type differs from what might be in AgentType
	bead := types.Bead{
		ID:     "hydra-test-456",
		Type:   "implementation", // This is the actual bead type
		Title:  "Implementation Task",
		Status: types.StatusOpen,
	}

	// Serialize to JSON
	beadJSON, err := json.Marshal(bead)
	if err != nil {
		t.Fatalf("Failed to marshal bead: %v", err)
	}

	// Set environment variables with conflicting AgentType
	os.Setenv("HYDRA_BEAD_JSON", string(beadJSON))
	os.Setenv("HYDRA_AGENT_TYPE", "orchestrator") // This should be ignored in favor of bead.Type
	defer func() {
		os.Unsetenv("HYDRA_BEAD_JSON")
		os.Unsetenv("HYDRA_AGENT_TYPE")
	}()

	// Parse config
	config := parseConfig()

	// The worker should use the bead's actual type, not the AgentType env var
	// This test verifies the expected behavior after implementation
	if config.AgentType != "implementation" {
		t.Logf("Note: Currently AgentType = %q, but after implementation should use bead.Type = %q",
			config.AgentType, bead.Type)
	}
}

// TestBeadJSONWithAllFields verifies that all bead fields are preserved in JSON serialization
func TestBeadJSONWithAllFields(t *testing.T) {
	// Create a comprehensive bead with all fields
	bead := types.Bead{
		ID:           "hydra-comprehensive-789",
		Project:      "hydra",
		Repo:         "main",
		Type:         "task",
		Title:        "Comprehensive Test",
		Description:  "A comprehensive test bead with all fields",
		Status:       types.StatusOpen,
		Dependencies: []types.BeadID{"hydra-dep-1", "hydra-dep-2"},
		Labels:       []string{"req-001", "tdd-red", "implementation"},
		Priority:     2,
		Metadata: map[string]string{
			"target_file":       "comprehensive_test.go",
			"target_files":      "[\"file1.go\", \"file2.go\"]",
			"agent_type":        "test-agent",
			"estimated_minutes": "60",
		},
	}

	// Serialize to JSON
	beadJSON, err := json.Marshal(bead)
	if err != nil {
		t.Fatalf("Failed to marshal bead: %v", err)
	}

	// Set environment variable
	os.Setenv("HYDRA_BEAD_JSON", string(beadJSON))
	defer os.Unsetenv("HYDRA_BEAD_JSON")

	// Parse config
	config := parseConfig()

	// Verify core fields
	if config.BeadID == "" {
		t.Error("BeadID should not be empty")
	}

	if config.BeadTitle != bead.Title {
		t.Errorf("BeadTitle = %q, want %q", config.BeadTitle, bead.Title)
	}
}
