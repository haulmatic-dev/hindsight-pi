package validation

import (
	"context"
	"fmt"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadsClient defines the interface needed for bead operations
type BeadsClient interface {
	GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error)
	CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error
}

// CloseBeadWithValidation wraps bead closure with validation checks
// It validates the implementation before allowing the bead to be closed
func CloseBeadWithValidation(
	beadsClient BeadsClient,
	validator *Validator,
	projectPath, repoName string,
	beadID types.BeadID,
	reason string,
) error {
	// Get the bead to check if validation is needed
	bead, err := beadsClient.GetBead(projectPath, repoName, beadID)
	if err != nil {
		return fmt.Errorf("failed to get bead for validation: %w", err)
	}

	// Check if this is end-of-chain (final implementation bead)
	if !IsEndOfChain(*bead) {
		// Not an implementation bead, close without validation
		return beadsClient.CloseBead(projectPath, repoName, beadID, reason)
	}

	// Run validation
	ctx := context.Background()
	result, err := validator.Validate(ctx, *bead, projectPath)
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	// If validation failed, block closure
	if !result.Passed {
		return fmt.Errorf("VALIDATION FAILED - Bead NOT closed:\n%s", result.FormatResult())
	}

	// Validation passed, proceed with closure
	return beadsClient.CloseBead(projectPath, repoName, beadID, reason)
}
