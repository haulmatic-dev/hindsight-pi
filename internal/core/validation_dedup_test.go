package core

import (
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Validation churn factory: RunPRDValidation/RunTDDValidation created fix
// beads unconditionally — no check for an already-open fix bead covering the
// same gap. On srv-smart-engine the "No Implementation Files" Critical gap
// (leaf beads lack target_files metadata) regenerated a fix bead plus a
// re-validate bead every round: 355 open churn beads accumulated while only
// 36 real chain beads existed. Closing a fix bead never fixes the parent, so
// the same gap re-detects forever.
//
// Dedup rule: before creating a fix bead, skip when an OPEN or IN_PROGRESS
// bead already exists with the same round-insensitive title prefix
// ("Fix: <gap title>"). CLOSED fix beads do not block respawn — if the gap
// persists after a closed fix, the retry loop (capped by max_rounds) is the
// intended escalation path.

func countBeadsWithPrefix(beads map[string]types.Bead, prefix string) int {
	n := 0
	for _, b := range beads {
		if len(b.Title) >= len(prefix) && b.Title[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

func TestRunPRDValidation_DedupsOpenFixBeads(t *testing.T) {
	config := DefaultConfig()
	config.Validation.PRDValidationEnabled = true
	config.Validation.MaxRounds = 5

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	projectPath := t.TempDir()

	rootBead := types.Bead{
		ID:          "test-root",
		Type:        "requirements",
		Title:       "Feature X",
		Description: "## Acceptance Criteria\n- Must do A",
		Project:     filepath.Base(projectPath),
		Status:      types.StatusClosed,
	}
	// Leaf references a file that does not exist -> "Missing Implementation
	// File" Error gap every round.
	leafBead := types.Bead{
		ID:       "test-leaf",
		Type:     "implement",
		Title:    "Implement Feature X",
		Project:  filepath.Base(projectPath),
		Metadata: map[string]string{"target_files": "missing_feature.go"},
		Status:   types.StatusClosed,
	}
	mockBeads.addBead(rootBead)
	mockBeads.addBead(leafBead)

	chain := &ChainInfo{
		RootBeadID:  "test-root",
		LeafBeadIDs: []types.BeadID{"test-leaf"},
		AllBeadIDs:  []types.BeadID{"test-root", "test-leaf"},
	}

	if err := o.RunPRDValidation(chain, projectPath); err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstTotal := len(mockBeads.beads)
	if countBeadsWithPrefix(mockBeads.beads, "Fix: ") == 0 {
		t.Fatalf("first run created no fix beads; test setup broken")
	}

	// Second validation run: same gap still present, fix bead from round 1
	// still OPEN -> must not create another fix or re-validate bead.
	if err := o.RunPRDValidation(chain, projectPath); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(mockBeads.beads) != firstTotal {
		t.Errorf("second run created %d new beads despite open duplicate, want 0",
			len(mockBeads.beads)-firstTotal)
	}
}

func TestRunPRDValidation_RespawnsAfterFixClosed(t *testing.T) {
	config := DefaultConfig()
	config.Validation.PRDValidationEnabled = true
	config.Validation.MaxRounds = 5

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	projectPath := t.TempDir()

	rootBead := types.Bead{
		ID:          "test-root",
		Type:        "requirements",
		Title:       "Feature X",
		Description: "## Acceptance Criteria\n- Must do A",
		Project:     filepath.Base(projectPath),
		Status:      types.StatusClosed,
	}
	leafBead := types.Bead{
		ID:       "test-leaf",
		Type:     "implement",
		Title:    "Implement Feature X",
		Project:  filepath.Base(projectPath),
		Metadata: map[string]string{"target_files": "missing_feature.go"},
		Status:   types.StatusClosed,
	}
	mockBeads.addBead(rootBead)
	mockBeads.addBead(leafBead)

	chain := &ChainInfo{
		RootBeadID:  "test-root",
		LeafBeadIDs: []types.BeadID{"test-leaf"},
		AllBeadIDs:  []types.BeadID{"test-root", "test-leaf"},
	}

	if err := o.RunPRDValidation(chain, projectPath); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Operator closes the round-1 fix bead (gap not actually resolved).
	for id, b := range mockBeads.beads {
		if len(b.Title) >= 5 && b.Title[:5] == "Fix: " {
			b.Status = types.StatusClosed
			mockBeads.beads[id] = b
		}
	}

	before := len(mockBeads.beads)
	if err := o.RunPRDValidation(chain, projectPath); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(mockBeads.beads) <= before {
		t.Errorf("closed fix bead blocked respawn; retry loop must stay alive (capped by max_rounds)")
	}
}

func TestRunTDDValidation_DedupsOpenFixBeads(t *testing.T) {
	config := DefaultConfig()
	config.Validation.TDDValidationEnabled = true
	config.Validation.MaxRounds = 5

	o := NewOverseer(config)
	mockBeads := newMockBeadsClient()
	o.beadsClient = mockBeads
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)

	projectPath := t.TempDir()

	redBead := types.Bead{
		ID:       "test-red",
		Type:     "test",
		Title:    "RED: Write tests",
		Project:  filepath.Base(projectPath),
		Metadata: map[string]string{"target_files": "missing_test.go"},
		Status:   types.StatusClosed,
	}
	greenBead := types.Bead{
		ID:       "test-green",
		Type:     "implement",
		Title:    "GREEN: Implement feature",
		Project:  filepath.Base(projectPath),
		Metadata: map[string]string{"target_files": "missing_feature.go"},
		Status:   types.StatusClosed,
	}
	refactorBead := types.Bead{
		ID:           "test-refactor",
		Type:         "refactor",
		Title:        "Refactor feature",
		Project:      filepath.Base(projectPath),
		Dependencies: []types.BeadID{"test-red", "test-green"},
		Status:       types.StatusInProgress,
	}
	mockBeads.addBead(redBead)
	mockBeads.addBead(greenBead)
	mockBeads.addBead(refactorBead)

	if err := o.RunTDDValidation(refactorBead, projectPath); err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstTotal := len(mockBeads.beads)
	if countBeadsWithPrefix(mockBeads.beads, "Fix: ") == 0 {
		t.Fatalf("first run created no fix beads; test setup broken")
	}

	if err := o.RunTDDValidation(refactorBead, projectPath); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(mockBeads.beads) != firstTotal {
		t.Errorf("second run created %d new fix beads despite open duplicate, want 0",
			len(mockBeads.beads)-firstTotal)
	}
}
