package validation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
			name: "RED phase bead with target files",
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
			expected: true, // Stub fix: RED now also validates (test files must exist + non-empty)
		},
		{
			name: "GREEN phase bead with target files",
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
			expected: true, // Stub fix: GREEN now validates (this is where stubs most often land)
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

func TestTDDPhase(t *testing.T) {
	tests := []struct {
		name     string
		labels   []string
		expected string
	}{
		{name: "no labels", labels: nil, expected: ""},
		{name: "tdd-red", labels: []string{"tdd-red", "testing"}, expected: "red"},
		{name: "tdd-green", labels: []string{"tdd-green", "implementation"}, expected: "green"},
		{name: "tdd-refactor", labels: []string{"tdd-refactor", "cleanup"}, expected: "refactor"},
		{name: "tdd-syntax", labels: []string{"tdd-syntax", "quality"}, expected: "syntax"},
		{name: "unrelated labels", labels: []string{"fr", "p1"}, expected: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bead := types.Bead{ID: "x", Labels: tt.labels}
			if got := TDDPhase(bead); got != tt.expected {
				t.Errorf("TDDPhase() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCloseBeadWithValidation_RunsOnTDDPhaseBeads(t *testing.T) {
	// Stub fix: this test now asserts the inverse of its previous
	// behaviour. RED/GREEN/REFACTOR/SYNTAX beads ALL go through
	// validation; they are not exempt because the static check
	// (file existence + non-empty + no panic stubs) is exactly what
	// catches a stub.
	mockClient := NewMockBeadsClient()
	validator := NewValidator(DefaultValidationConfig())

	// Set up a real Go module in a temp dir so compilation/tests
	// can run. The implementation file has a real body (not a
	// stub) and the test file asserts something meaningful.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module stubtest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	realImpl := `package testpkg
type S struct{}
func (s *S) DoWork(x int) int { return x + 1 }
`
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte(realImpl), 0o644); err != nil {
		t.Fatal(err)
	}
	testImpl := `package testpkg
import "testing"
func TestDoWork(t *testing.T) {
	s := &S{}
	if got := s.DoWork(2); got != 3 {
		t.Errorf("DoWork(2) = %d, want 3", got)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "test_test.go"), []byte(testImpl), 0o644); err != nil {
		t.Fatal(err)
	}

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

	err := CloseBeadWithValidation(
		mockClient,
		validator,
		dir,
		"test-repo",
		bead.ID,
		"completed",
	)

	if err != nil {
		t.Errorf("CloseBeadWithValidation() for RED bead with real impl should not error, got: %v", err)
	}
}

func TestCloseBeadWithValidation_BlocksStubImplementation(t *testing.T) {
	// The regression we're fixing: the legacy validator let stubs
	// through. The new validator MUST reject a target file whose
	// only function body is `return nil, nil` (or `return 0, nil`,
	// etc.) and report a stub pattern.
	mockClient := NewMockBeadsClient()
	validator := NewValidator(DefaultValidationConfig())

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module stubtest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deliberately stub-like: empty body returning zero values.
	stubImpl := `package stubtest
type Activities struct{}
// GetThing is the production implementation. Real implementation
// comes in the follow-up bead; today this is a stub.
func (a *Activities) GetThing(_ /* ctx */ interface{}, id string) (string, error) {
	_ = id
	return "", nil
}
`
	if err := os.WriteFile(filepath.Join(dir, "stub.go"), []byte(stubImpl), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stub_test.go"), []byte("package stubtest\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bead := &types.Bead{
		ID:      "stub-test",
		Title:   "GREEN: stub feature",
		Type:    "implement",
		Labels:  []string{"tdd-green"},
		Status:  types.StatusOpen,
		Metadata: map[string]string{"target_files": "stub.go"},
	}
	mockClient.AddBead(bead)

	err := CloseBeadWithValidation(
		mockClient,
		validator,
		dir,
		"test-repo",
		bead.ID,
		"completed",
	)

	if err == nil {
		t.Fatal("expected validation to fail on stub implementation; got nil error")
	}
	if !strings.Contains(err.Error(), "stub") && !strings.Contains(err.Error(), "Stub") {
		t.Errorf("expected error message to mention 'stub'; got: %v", err)
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
