package validation

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// "No Implementation Files" fires purely from missing target_files metadata.
// When every leaf closed through verified-close (diff gate), the work
// demonstrably happened — the gap is metadata-only and must be advisory
// (Warning, no fix bead). Live evidence: kgkc7 burned 20.5M tokens wandering
// the repo on exactly this phantom (2026-08-21). Only leaves that have NOT
// all closed make it Critical.
func TestValidateChain_NoImplFiles_MetadataOnlyIsWarning(t *testing.T) {
	v := NewPRDValidator(ValidationConfig{})
	root := types.Bead{ID: "proj-root1", Title: "root", Description: "## Acceptance Criteria\n- x"}
	closed := types.Bead{ID: "proj-leaf1", Status: types.StatusClosed}

	res, err := v.ValidateChainAgainstPRD(root, []types.Bead{closed}, t.TempDir())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, g := range res.Gaps {
		if g.Title == "No Implementation Files" && g.Severity == SeverityCritical {
			t.Errorf("metadata-only gap on fully-closed chain must be Warning, got Critical")
		}
	}
}

func TestValidateChain_NoImplFiles_OpenLeavesStayCritical(t *testing.T) {
	v := NewPRDValidator(ValidationConfig{})
	root := types.Bead{ID: "proj-root1", Title: "root", Description: "## Acceptance Criteria\n- x"}
	open := types.Bead{ID: "proj-leaf1", Status: types.StatusOpen}

	res, err := v.ValidateChainAgainstPRD(root, []types.Bead{open}, t.TempDir())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	found := false
	for _, g := range res.Gaps {
		if g.Title == "No Implementation Files" && g.Severity == SeverityCritical {
			found = true
		}
	}
	if !found {
		t.Errorf("gap with open leaves must stay Critical")
	}
}
