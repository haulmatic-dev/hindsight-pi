package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/validation"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestRunTDDValidation_SkipsWhenDisabled(t *testing.T) {
	config := DefaultConfig()
	config.Validation.Enabled = false

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	refactorBead := types.Bead{
		ID:   "test-refactor",
		Type: "refactor",
	}

	err := o.RunTDDValidation(refactorBead, "/tmp")
	if err != nil {
		t.Fatalf("Expected no error when disabled, got %v", err)
	}
}

func TestRunTDDValidation_NoRedGreenBeads(t *testing.T) {
	config := DefaultConfig()
	config.Validation.TDDValidationEnabled = true
	config.Validation.MaxRounds = 3

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	refactorBead := types.Bead{
		ID:           "test-refactor",
		Type:         "refactor",
		Title:        "Refactor feature",
		Dependencies: []types.BeadID{},
	}

	err := o.RunTDDValidation(refactorBead, "/tmp")
	if err != nil {
		t.Fatalf("Expected no error when no RED/GREEN beads, got %v", err)
	}
}

func TestRunTDDValidation_CreatesFixBeadOnGap(t *testing.T) {
	config := DefaultConfig()
	config.Validation.TDDValidationEnabled = true
	config.Validation.MaxRounds = 3

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	projectPath := t.TempDir()

	// Create RED bead with a test file that lacks meaningful assertions
	redBead := types.Bead{
		ID:          "test-red",
		Type:        "test",
		Title:       "RED: Write tests",
		Project:     "test",
		Metadata:    map[string]string{"target_files": "test_feature.go"},
		Status:      types.StatusClosed,
	}

	// Create GREEN bead with missing implementation file
	greenBead := types.Bead{
		ID:          "test-green",
		Type:        "implement",
		Title:       "GREEN: Implement feature",
		Project:     "test",
		Metadata:    map[string]string{"target_files": "feature.go"},
		Status:      types.StatusClosed,
	}

	refactorBead := types.Bead{
		ID:           "test-refactor",
		Type:         "refactor",
		Title:        "Refactor feature",
		Project:      "test",
		Dependencies: []types.BeadID{"test-red", "test-green"},
		Status:       types.StatusInProgress,
	}

	mockBeads.addBead(redBead)
	mockBeads.addBead(greenBead)
	mockBeads.addBead(refactorBead)

	err := o.RunTDDValidation(refactorBead, projectPath)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should have created fix beads
	if o.retryManager.GetValidationRetryCount("test-refactor") != 1 {
		t.Errorf("Expected validation retry count 1, got %d", o.retryManager.GetValidationRetryCount("test-refactor"))
	}
}

func TestRunTDDValidation_ExhaustedRetries(t *testing.T) {
	config := DefaultConfig()
	config.Validation.TDDValidationEnabled = true
	config.Validation.MaxRounds = 1

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	projectPath := t.TempDir()

	redBead := types.Bead{
		ID:          "test-red",
		Type:        "test",
		Title:       "RED: Write tests",
		Project:     "test",
		Metadata:    map[string]string{"target_files": "test_feature.go"},
		Status:      types.StatusClosed,
	}

	greenBead := types.Bead{
		ID:          "test-green",
		Type:        "implement",
		Title:       "GREEN: Implement feature",
		Project:     "test",
		Metadata:    map[string]string{"target_files": "feature.go"},
		Status:      types.StatusClosed,
	}

	refactorBead := types.Bead{
		ID:           "test-refactor",
		Type:         "refactor",
		Title:        "Refactor feature",
		Project:      "test",
		Dependencies: []types.BeadID{"test-red", "test-green"},
		Status:       types.StatusInProgress,
	}

	mockBeads.addBead(redBead)
	mockBeads.addBead(greenBead)
	mockBeads.addBead(refactorBead)

	// First call uses retry 0, creates fix beads, increments to 1
	o.RunTDDValidation(refactorBead, projectPath)

	// Second call: retry count 1 >= maxRounds 1, exhausted, no increment
	o.RunTDDValidation(refactorBead, projectPath)

	if o.retryManager.GetValidationRetryCount("test-refactor") != 1 {
		t.Errorf("Expected validation retry count 1, got %d", o.retryManager.GetValidationRetryCount("test-refactor"))
	}
}

func TestRunPRDValidation_SkipsWhenDisabled(t *testing.T) {
	config := DefaultConfig()
	config.Validation.Enabled = false

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	chain := &ChainInfo{
		RootBeadID: "test-root",
	}

	err := o.RunPRDValidation(chain, "/tmp")
	if err != nil {
		t.Fatalf("Expected no error when disabled, got %v", err)
	}
}

func TestPRDValidator_Direct(t *testing.T) {
	projectPath := t.TempDir()

	rootBead := types.Bead{
		ID:          "test-root",
		Type:        "requirements",
		Title:       "Feature X",
		Description: "## Acceptance Criteria\n- Must do A\n- Must do B",
		Project:     "test",
	}

	leafBead := types.Bead{
		ID:       "test-leaf",
		Type:     "implement",
		Title:    "Implement Feature X",
		Project:  "test",
		Metadata: map[string]string{"target_files": "feature.go"},
	}

	validator := validation.NewPRDValidator(validation.ValidationConfig{
		Enabled:                  true,
		AgentVerificationEnabled: false,
	})

	result, err := validator.ValidateChainAgainstPRD(rootBead, []types.Bead{leafBead}, projectPath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	t.Logf("PRD validation result: passed=%v, gaps=%d, checkedFiles=%v", result.Passed, len(result.Gaps), result.CheckedFiles)
}

func TestRunPRDValidation_CreatesFixBeads(t *testing.T) {
	config := DefaultConfig()
	config.Validation.PRDValidationEnabled = true
	config.Validation.MaxRounds = 3

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	projectPath := t.TempDir()

	rootBead := types.Bead{
		ID:          "test-root",
		Type:        "requirements",
		Title:       "Feature X",
		Description: "## Acceptance Criteria\n- Must do A\n- Must do B",
		Project:     "test",
		Status:      types.StatusClosed,
	}

	leafBead := types.Bead{
		ID:          "test-leaf",
		Type:        "implement",
		Title:       "Implement Feature X",
		Project:     "test",
		Metadata:    map[string]string{"target_files": "feature.go"},
		Status:      types.StatusClosed,
	}

	mockBeads.addBead(rootBead)
	mockBeads.addBead(leafBead)

	chain := &ChainInfo{
		RootBeadID:  "test-root",
		LeafBeadIDs: []types.BeadID{"test-leaf"},
		AllBeadIDs:  []types.BeadID{"test-root", "test-leaf"},
	}

	err := o.RunPRDValidation(chain, projectPath)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should have created fix beads and re-validation bead
	if o.retryManager.GetValidationRetryCount("test-root") != 1 {
		t.Errorf("Expected validation retry count 1, got %d", o.retryManager.GetValidationRetryCount("test-root"))
	}
}

func TestRunPRDValidation_WithExistingFiles(t *testing.T) {
	config := DefaultConfig()
	config.Validation.PRDValidationEnabled = true
	config.Validation.MaxRounds = 3

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	projectPath := t.TempDir()

	// Create actual implementation file
	implFile := filepath.Join(projectPath, "feature.go")
	os.WriteFile(implFile, []byte("package main\n\nfunc Feature() {}"), 0644)

	rootBead := types.Bead{
		ID:          "test-root",
		Type:        "requirements",
		Title:       "Feature X",
		Description: "## Acceptance Criteria\n- Must do A",
		Project:     "test",
		Status:      types.StatusClosed,
	}

	leafBead := types.Bead{
		ID:          "test-leaf",
		Type:        "implement",
		Title:       "Implement Feature X",
		Project:     "test",
		Metadata:    map[string]string{"target_files": "feature.go"},
		Status:      types.StatusClosed,
	}

	mockBeads.addBead(rootBead)
	mockBeads.addBead(leafBead)

	chain := &ChainInfo{
		RootBeadID:  "test-root",
		LeafBeadIDs: []types.BeadID{"test-leaf"},
		AllBeadIDs:  []types.BeadID{"test-root", "test-leaf"},
	}

	err := o.RunPRDValidation(chain, projectPath)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// With existing files, static check passes. Agent verification may still create gaps
	// but for this test we just verify no panic/error
}
