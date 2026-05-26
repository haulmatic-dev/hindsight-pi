package core

import (
	"fmt"
	"log"
	"strings"

	"github.com/haulmatic-dev/hydra/internal/validation"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RunTDDValidation validates a TDD chain after REFACTOR completes.
// If validation fails and retries remain, it creates fix beads.
func (o *Overseer) RunTDDValidation(refactorBead types.Bead, projectPath string) error {
	if !o.config.Validation.Enabled || !o.config.Validation.TDDValidationEnabled {
		return nil
	}

	beadID := string(refactorBead.ID)

	// Find RED and GREEN beads in the chain (they should be dependencies of REFACTOR)
	redBead, greenBead, err := o.findTDDChainBeads(refactorBead)
	if err != nil {
		log.Printf("[TDDValidation] Could not find RED/GREEN beads for %s: %v", beadID, err)
		return nil // Skip validation if chain structure is unclear
	}

	// Run validation
	tddValidator := validation.NewTDDValidator(validation.ValidationConfig{
		Enabled:                  o.config.Validation.Enabled,
		AgentVerificationEnabled: o.config.Validation.AgentVerificationEnabled,
	})

	result, err := tddValidator.ValidateTDDChain(redBead, greenBead, refactorBead, projectPath)
	if err != nil {
		log.Printf("[TDDValidation] Validation error for %s: %v", beadID, err)
		return nil // Don't block on validation error
	}

	if result.Passed {
		log.Printf("[TDDValidation] PASSED for %s", beadID)
		o.clearValidationRetry(beadID)
		return nil
	}

	// Validation failed - check retry count
	retryCount := o.getValidationRetryCount(beadID)
	maxRounds := o.config.Validation.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}

	if retryCount >= maxRounds {
		log.Printf("[TDDValidation] Bead %s exhausted %d/%d validation rounds, proceeding", beadID, retryCount, maxRounds)
		return nil
	}

	// Create fix beads for gaps
	log.Printf("[TDDValidation] FAILED for %s (round %d/%d), creating fix beads for %d gaps", beadID, retryCount+1, maxRounds, len(result.Gaps))

	for _, gap := range result.Gaps {
		fixType := "fix"
		if gap.Type == "red" {
			fixType = "test" // Re-create tests
		}

		fixTitle := fmt.Sprintf("Fix: %s (validation round %d)", refactorBead.Title, retryCount+1)
		fixDesc := fmt.Sprintf("TDD validation gap (%s): %s\n\nOriginal bead: %s", gap.Type, gap.Description, refactorBead.ID)

		req := types.CreateBeadRequest{
			Title:        fixTitle,
			Description:  fixDesc,
			Type:         fixType,
			Priority:     refactorBead.Priority,
			Dependencies: []types.BeadID{refactorBead.ID},
			Labels:       []string{"auto-fix", fmt.Sprintf("tdd-validate-round-%d", retryCount+1)},
		}

		fixBeadID, err := o.beadsClient.CreateBead(projectPath, req)
		if err != nil {
			log.Printf("[TDDValidation] Failed to create fix bead for %s: %v", beadID, err)
			continue
		}
		log.Printf("[TDDValidation] Created fix bead %s for gap in %s", fixBeadID, beadID)
	}

	o.incrementValidationRetry(beadID)
	return nil
}

// RunPRDValidation validates that a completed chain satisfies its root PRD.
// Called by ChainCompletionDetector when a chain completes.
func (o *Overseer) RunPRDValidation(chainInfo *ChainInfo, projectPath string) error {
	log.Printf("[PRDValidation] DEBUG START enabled=%v prd=%v maxRounds=%d", o.config.Validation.Enabled, o.config.Validation.PRDValidationEnabled, o.config.Validation.MaxRounds)
	if !o.config.Validation.Enabled || !o.config.Validation.PRDValidationEnabled {
		return nil
	}

	rootBeadID := chainInfo.RootBeadID
	rootBead, err := o.beadsClient.GetBead(projectPath, "", rootBeadID)
	if err != nil {
		log.Printf("[PRDValidation] Could not get root bead %s: %v", rootBeadID, err)
		return nil
	}

	// Collect leaf beads
	leafBeads := make([]types.Bead, 0, len(chainInfo.LeafBeadIDs))
	for _, leafID := range chainInfo.LeafBeadIDs {
		bead, err := o.beadsClient.GetBead(projectPath, "", leafID)
		if err != nil {
			log.Printf("[PRDValidation] Could not get leaf bead %s: %v", leafID, err)
			continue
		}
		leafBeads = append(leafBeads, *bead)
	}

	prdValidator := validation.NewPRDValidator(validation.ValidationConfig{
		Enabled:                  o.config.Validation.Enabled,
		AgentVerificationEnabled: o.config.Validation.AgentVerificationEnabled,
	})

	result, err := prdValidator.ValidateChainAgainstPRD(*rootBead, leafBeads, projectPath)
	if err != nil {
		log.Printf("[PRDValidation] Validation error for chain %s: %v", rootBeadID, err)
		return nil
	}

	if result.Passed {
		log.Printf("[PRDValidation] PASSED for chain %s", rootBeadID)
		o.clearValidationRetry(string(rootBeadID))
		return nil
	}

	// Validation failed - check retry count
	retryCount := o.getValidationRetryCount(string(rootBeadID))
	maxRounds := o.config.Validation.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}

	if retryCount >= maxRounds {
		log.Printf("[PRDValidation] Chain %s exhausted %d/%d validation rounds, marking complete", rootBeadID, retryCount, maxRounds)
		return nil
	}

	// Create fix beads for gaps
	log.Printf("[PRDValidation] FAILED for chain %s (round %d/%d), creating fix beads for %d gaps", rootBeadID, retryCount+1, maxRounds, len(result.Gaps))

	fixBeadIDs := make([]types.BeadID, 0, len(result.Gaps))
	for _, gap := range result.Gaps {
		fixTitle := fmt.Sprintf("Fix: %s (validation round %d)", gap.Title, retryCount+1)
		fixDesc := fmt.Sprintf("PRD validation gap: %s\n\nRoot bead: %s", gap.Description, rootBeadID)

		req := types.CreateBeadRequest{
			Title:        fixTitle,
			Description:  fixDesc,
			Type:         "fix",
			Priority:     rootBead.Priority,
			Dependencies: []types.BeadID{}, // Will depend on chain completion
			Labels:       []string{"auto-fix", fmt.Sprintf("prd-validate-round-%d", retryCount+1)},
		}

		fixBeadID, err := o.beadsClient.CreateBead(projectPath, req)
		if err != nil {
			log.Printf("[PRDValidation] Failed to create fix bead for chain %s: %v", rootBeadID, err)
			continue
		}
		fixBeadIDs = append(fixBeadIDs, fixBeadID)
		log.Printf("[PRDValidation] Created fix bead %s for gap in chain %s", fixBeadID, rootBeadID)
	}

	// Create re-validation bead that depends on all fix beads
	if len(fixBeadIDs) > 0 {
		validateTitle := fmt.Sprintf("Re-validate: %s (round %d)", rootBead.Title, retryCount+1)
		validateDesc := fmt.Sprintf("Re-validate chain against PRD after fixes\n\nRoot bead: %s", rootBeadID)

		req := types.CreateBeadRequest{
			Title:        validateTitle,
			Description:  validateDesc,
			Type:         "validation",
			Priority:     rootBead.Priority,
			Dependencies: fixBeadIDs,
			Labels:       []string{"auto-validate", fmt.Sprintf("prd-validate-round-%d", retryCount+1)},
		}

		validateBeadID, err := o.beadsClient.CreateBead(projectPath, req)
		if err != nil {
			log.Printf("[PRDValidation] Failed to create re-validation bead for chain %s: %v", rootBeadID, err)
		} else {
			log.Printf("[PRDValidation] Created re-validation bead %s for chain %s", validateBeadID, rootBeadID)
		}
	}

	o.incrementValidationRetry(string(rootBeadID))
	return nil
}

// findTDDChainBeads locates the RED and GREEN beads that are dependencies of the REFACTOR bead
func (o *Overseer) findTDDChainBeads(refactorBead types.Bead) (types.Bead, types.Bead, error) {
	var redBead, greenBead types.Bead
	var redFound, greenFound bool

	for _, depID := range refactorBead.Dependencies {
		bead, err := o.beadsClient.GetBead(refactorBead.Project, refactorBead.Repo, depID)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(bead.Title), "red") || bead.Type == "test" {
			redBead = *bead
			redFound = true
		}
		if strings.Contains(strings.ToLower(bead.Title), "green") || bead.Type == "implement" {
			greenBead = *bead
			greenFound = true
		}
	}

	if !redFound && !greenFound {
		return types.Bead{}, types.Bead{}, fmt.Errorf("no RED or GREEN beads found as dependencies of %s", refactorBead.ID)
	}

	return redBead, greenBead, nil
}
