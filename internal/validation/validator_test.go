package validation

import (
	"context"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// MockBeadsClient implements BeadsClient for testing
type MockBeadsClient struct {
	beads map[string]*types.Bead
}

func NewMockBeadsClient() *MockBeadsClient {
	return &MockBeadsClient{
		beads: make(map[string]*types.Bead),
	}
}

func (m *MockBeadsClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	if bead, ok := m.beads[string(beadID)]; ok {
		return bead, nil
	}
	return nil, nil
}

func (m *MockBeadsClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	return nil
}

func (m *MockBeadsClient) AddBead(bead *types.Bead) {
	m.beads[string(bead.ID)] = bead
}

func TestIsEndOfChain(t *testing.T) {
	tests := []struct {
		name     string
		bead     types.Bead
		expected bool
	}{
		{
			name: "Implementation bead with target files",
			bead: types.Bead{
				ID:     "hydra-test",
				Title:  "Test Implementation",
				Type:   "implement",
				Labels: []string{}, // No TDD labels
				Status: types.StatusOpen,
				Metadata: map[string]string{
					"target_files": "internal/test/file.go",
				},
			},
			expected: true,
		},
		{
			name: "RED phase bead",
			bead: types.Bead{
				ID:     "hydra-test",
				Title:  "Write tests",
				Type:   "test",
				Labels: []string{"tdd-red"},
				Status: types.StatusOpen,
				Metadata: map[string]string{
					"target_files": "internal/test/file_test.go",
				},
			},
			expected: false,
		},
		{
			name: "GREEN phase bead",
			bead: types.Bead{
				ID:     "hydra-test",
				Title:  "Implement feature",
				Type:   "implement",
				Labels: []string{"tdd-green"},
				Status: types.StatusOpen,
				Metadata: map[string]string{
					"target_files": "internal/test/file.go",
				},
			},
			expected: false,
		},
		{
			name: "No target files",
			bead: types.Bead{
				ID:       "hydra-test",
				Title:    "Research task",
				Type:     "research",
				Labels:   []string{},
				Status:   types.StatusOpen,
				Metadata: map[string]string{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsEndOfChain(tt.bead)
			if result != tt.expected {
				t.Errorf("IsEndOfChain() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCloseBeadWithValidation_SkipsNonImplementationBeads(t *testing.T) {
	mockClient := NewMockBeadsClient()
	validator := NewValidator(DefaultValidationConfig())

	// Create a RED phase bead (should skip validation)
	bead := &types.Bead{
		ID:     "hydra-test-red",
		Title:  "Write tests",
		Type:   "test",
		Labels: []string{"tdd-red"},
		Status: types.StatusOpen,
		Metadata: map[string]string{
			"target_files": "test.go",
		},
	}
	mockClient.AddBead(bead)

	// Should close without validation error
	err := CloseBeadWithValidation(
		mockClient,
		validator,
		"/test/project",
		"test-repo",
		bead.ID,
		"completed",
	)

	if err != nil {
		t.Errorf("CloseBeadWithValidation() for RED bead should not error, got: %v", err)
	}
}

func TestValidator_Validate_Quick(t *testing.T) {
	validator := NewValidator(DefaultValidationConfig())

	bead := types.Bead{
		ID:     "hydra-test",
		Title:  "Test Bead",
		Status: types.StatusOpen,
		Metadata: map[string]string{
			"target_files": "nonexistent.go",
		},
	}

	ctx := context.Background()
	result, err := validator.ValidateQuick(ctx, bead, "/tmp")

	if err != nil {
		t.Fatalf("ValidateQuick() error = %v", err)
	}

	// Should fail because file doesn't exist
	if result.Passed {
		t.Error("ValidateQuick() should fail for nonexistent file")
	}

	// Should have errors
	if len(result.Errors) == 0 {
		t.Error("ValidateQuick() should return errors for nonexistent file")
	}
}

func TestValidationResult_FormatResult(t *testing.T) {
	result := &ValidationResult{
		Passed:       false,
		Phase:        "static",
		StaticPassed: false,
		Errors: []ValidationError{
			{
				Severity: SeverityCritical,
				Message:  "Test error",
				File:     "test.go",
				Line:     42,
				Category: "test",
			},
		},
		Warnings: []string{"Test warning"},
	}

	formatted := result.FormatResult()

	// Check that it contains expected elements
	if !contains(formatted, "VALIDATION FAILED") {
		t.Error("FormatResult() should contain 'VALIDATION FAILED'")
	}

	if !contains(formatted, "Test error") {
		t.Error("FormatResult() should contain error message")
	}

	if !contains(formatted, "Test warning") {
		t.Error("FormatResult() should contain warning")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
