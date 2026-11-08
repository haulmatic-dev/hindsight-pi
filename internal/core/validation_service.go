package core

import (
	"fmt"
	"log"
	"strings"

	"github.com/haulmatic-dev/hydra/internal/validation"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ValidationService orchestrates TDD, PRD, and end-of-chain validation.
type ValidationService struct {
	config       *Config
	retryManager *RetryManager
	beadsClient  BeadsClient
}

// NewValidationService creates a new validation service.
func NewValidationService(config *Config, retryManager *RetryManager, beadsClient BeadsClient) *ValidationService {
	return &ValidationService{
		config:       config,
		retryManager: retryManager,
		beadsClient:  beadsClient,
	}
}

// CloseBead closes a bead with optional end-of-chain validation.
func (v *ValidationService) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	if v.config.Validation.Enabled {
		validator := validation.NewValidator(validation.DefaultValidationConfig())
		return validation.CloseBeadWithValidation(
			v.beadsClient,
			validator,
			projectPath,
			repoName,
			beadID,
			reason,
		)
	}
	return v.beadsClient.CloseBead(projectPath, repoName, beadID, reason)
}

// validationFixDecision decides whether a validation gap warrants a fix
// bead, and at what priority. Warning gaps (e.g. "Missing Acceptance
// Criteria" — fires whenever the root bead lacks AC text) are advisory:
// logged, never a worker-burning fix bead. Spawned fix beads are floored at
// P3 so validation meta-work never queue-jumps fresh PRD beads — on
// srv-smart-engine these fix beads inherited P0-P2 parents and ~25% of a
// day's commits were validation-round churn while the open board grew.
// defaultValidationRounds matches the historical default for
// Validation.MaxRounds when the knob is unset or non-positive.
const defaultValidationRounds = 3

func validationFixDecision(severity validation.ErrorSeverity, parentPriority int) (bool, int) {
	if severity == validation.SeverityWarning {
		return false, 0
	}
	p := parentPriority
	if p < 3 {
		p = 3
	}
	return true, p
}

// validationRounds reports the retry round a bead has reached and the
// effective round cap, plus whether that cap is already exhausted.
func (v *ValidationService) validationRounds(beadID string) (round, maxRounds int, exhausted bool) {
	round = v.retryManager.GetValidationRetryCount(beadID)
	maxRounds = v.config.Validation.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultValidationRounds
	}
	return round, maxRounds, round >= maxRounds
}

// newTDDValidator builds a TDD validator from the service's knobs.
func (v *ValidationService) newTDDValidator() *validation.TDDValidator {
	return validation.NewTDDValidator(validation.ValidationConfig{
		Enabled:                  v.config.Validation.Enabled,
		AgentVerificationEnabled: v.config.Validation.AgentVerificationEnabled,
	})
}

// recordCreatedFix remembers a fix bead created in this round so a
// repeated gap title cannot spawn an identical sibling before the store
// is re-listed on the next validation round.
func recordCreatedFix(existing []types.Bead, fixTitle string) []types.Bead {
	return append(existing, types.Bead{Title: fixTitle, Status: types.StatusOpen})
}

// isTDDRole reports whether a dependency bead fills the given chain
// role, either by title keyword or by the bead type that role maps to.
func isTDDRole(bead *types.Bead, keyword, beadType string) bool {
	return strings.Contains(strings.ToLower(bead.Title), keyword) || bead.Type == beadType
}

// RunTDDValidation validates a REFACTOR bead's TDD chain and creates fix beads on failure.
// hasOpenBeadWithPrefix reports whether an OPEN or IN_PROGRESS bead already
// exists whose title starts with prefix. Validation fix titles embed the round
// number ("Fix: X (validation round 2)"), so dedup must compare on the
// round-insensitive prefix — otherwise every round spawns a fresh duplicate
// for the same gap. On srv-smart-engine this churn factory accumulated 355
// open machinery beads against 36 real chain beads. CLOSED duplicates do not
// block respawn: if the gap persists past a closed fix, the retry loop (capped
// by max_rounds) is the intended escalation.
func hasOpenBeadWithPrefix(beads []types.Bead, prefix string) bool {
	for _, b := range beads {
		if (b.Status == types.StatusOpen || b.Status == types.StatusInProgress) &&
			strings.HasPrefix(b.Title, prefix) {
			return true
		}
	}
	return false
}

// listForDedup snapshots the project's beads for duplicate detection. Listing
// failure is fail-closed for creation: skip this round's fix-bead pass (logged)
// rather than risk recreating the churn factory on a transient br error.
func (v *ValidationService) listForDedup(projectPath string) ([]types.Bead, bool) {
	beads, err := v.beadsClient.ListBeads(projectPath, "")
	if err != nil {
		log.Printf("[Validation] dedup listing failed, skipping fix-bead creation this round: %v", err)
		return nil, false
	}
	return beads, true
}

func (v *ValidationService) RunTDDValidation(refactorBead types.Bead, projectPath string) error {
	if !v.config.Validation.Enabled || !v.config.Validation.TDDValidationEnabled {
		return nil
	}

	beadID := string(refactorBead.ID)

	redBead, greenBead, err := v.findTDDChainBeads(refactorBead, projectPath)
	if err != nil {
		log.Printf("[TDDValidation] Could not find RED/GREEN beads for %s: %v", beadID, err)
		return nil
	}

	result, err := v.newTDDValidator().ValidateTDDChain(redBead, greenBead, refactorBead, projectPath)
	if err != nil {
		log.Printf("[TDDValidation] Validation error for %s: %v", beadID, err)
		return nil
	}

	if result.Passed {
		log.Printf("[TDDValidation] PASSED for %s", beadID)
		v.retryManager.ClearValidationRetry(beadID)
		return nil
	}

	retryCount, maxRounds, exhausted := v.validationRounds(beadID)
	if exhausted {
		log.Printf("[TDDValidation] Bead %s exhausted %d/%d validation rounds, proceeding", beadID, retryCount, maxRounds)
		return nil
	}

	log.Printf("[TDDValidation] FAILED for %s (round %d/%d), creating fix beads for %d gaps", beadID, retryCount+1, maxRounds, len(result.Gaps))

	existing, ok := v.listForDedup(projectPath)
	if !ok {
		return nil
	}
	fixPrefix := "Fix: " + refactorBead.Title

	for _, gap := range result.Gaps {
		spawn, fixPriority := validationFixDecision(gap.Severity, refactorBead.Priority)
		if !spawn {
			log.Printf("[TDDValidation] advisory gap (warning, no fix bead): %s", gap.Description)
			continue
		}
		if hasOpenBeadWithPrefix(existing, fixPrefix) {
			log.Printf("[TDDValidation] dedup: open fix bead already covers %q, skipping", refactorBead.Title)
			continue
		}
		fixType := "fix"
		if gap.Type == "red" {
			fixType = "test"
		}

		fixTitle := fmt.Sprintf("Fix: %s (validation round %d)", refactorBead.Title, retryCount+1)
		fixDesc := fmt.Sprintf("TDD validation gap (%s): %s\n\nOriginal bead: %s", gap.Type, gap.Description, refactorBead.ID)

		req := types.CreateBeadRequest{
			Title:        fixTitle,
			Description:  fixDesc,
			Type:         fixType,
			Priority:     fixPriority,
			Dependencies: []types.BeadID{refactorBead.ID},
			Labels:       []string{"auto-fix", fmt.Sprintf("tdd-validate-round-%d", retryCount+1)},
		}

		fixBeadID, err := v.beadsClient.CreateBead(projectPath, req)
		if err != nil {
			log.Printf("[TDDValidation] Failed to create fix bead for %s: %v", beadID, err)
			continue
		}
		existing = recordCreatedFix(existing, fixTitle)
		log.Printf("[TDDValidation] Created fix bead %s for gap in %s", fixBeadID, beadID)
	}

	v.retryManager.IncrementValidationRetry(beadID)
	return nil
}

// TDDChainPasses runs TDD-chain validation and reports whether it passed,
// without the fix-bead side effects of RunTDDValidation. The verified-close
// gate uses this to block a close when validation fails. It returns
// (true, nil) — i.e. "do not block" — when TDD validation is disabled, the
// bead is not a TDD bead, the chain cannot be located, or verification
// errors; only a definite validation failure returns (false, nil).
func (v *ValidationService) TDDChainPasses(refactorBead types.Bead, projectPath string) (bool, error) {
	if !v.config.Validation.Enabled || !v.config.Validation.TDDValidationEnabled {
		return true, nil
	}

	redBead, greenBead, err := v.findTDDChainBeads(refactorBead, projectPath)
	if err != nil {
		// Cannot locate the chain — do not hard-block on inability to verify.
		return true, nil
	}

	result, err := v.newTDDValidator().ValidateTDDChain(redBead, greenBead, refactorBead, projectPath)
	if err != nil {
		return true, err
	}
	return result.Passed, nil
}

// RunPRDValidation validates a completed chain against its root PRD and creates fix beads on failure.
func (v *ValidationService) RunPRDValidation(chainInfo *ChainInfo, projectPath string) error {
	log.Printf("[PRDValidation] DEBUG START enabled=%v prd=%v maxRounds=%d", v.config.Validation.Enabled, v.config.Validation.PRDValidationEnabled, v.config.Validation.MaxRounds)
	if !v.config.Validation.Enabled || !v.config.Validation.PRDValidationEnabled {
		return nil
	}

	rootBeadID := chainInfo.RootBeadID
	rootBead, err := v.beadsClient.GetBead(projectPath, "", rootBeadID)
	if err != nil {
		log.Printf("[PRDValidation] Could not get root bead %s: %v", rootBeadID, err)
		return nil
	}

	leafBeads := make([]types.Bead, 0, len(chainInfo.LeafBeadIDs))
	for _, leafID := range chainInfo.LeafBeadIDs {
		bead, err := v.beadsClient.GetBead(projectPath, "", leafID)
		if err != nil {
			log.Printf("[PRDValidation] Could not get leaf bead %s: %v", leafID, err)
			continue
		}
		leafBeads = append(leafBeads, *bead)
	}

	prdValidator := validation.NewPRDValidator(validation.ValidationConfig{
		Enabled:                  v.config.Validation.Enabled,
		AgentVerificationEnabled: v.config.Validation.AgentVerificationEnabled,
	})

	result, err := prdValidator.ValidateChainAgainstPRD(*rootBead, leafBeads, projectPath)
	if err != nil {
		log.Printf("[PRDValidation] Validation error for chain %s: %v", rootBeadID, err)
		return nil
	}

	if result.Passed {
		log.Printf("[PRDValidation] PASSED for chain %s", rootBeadID)
		v.retryManager.ClearValidationRetry(string(rootBeadID))
		return nil
	}

	retryCount, maxRounds, exhausted := v.validationRounds(string(rootBeadID))
	if exhausted {
		log.Printf("[PRDValidation] Chain %s exhausted %d/%d validation rounds, marking complete", rootBeadID, retryCount, maxRounds)
		return nil
	}

	log.Printf("[PRDValidation] FAILED for chain %s (round %d/%d), creating fix beads for %d gaps", rootBeadID, retryCount+1, maxRounds, len(result.Gaps))

	fixBeadIDs := make([]types.BeadID, 0, len(result.Gaps))
	existing, ok := v.listForDedup(projectPath)
	if !ok {
		return nil
	}

	for _, gap := range result.Gaps {
		spawn, fixPriority := validationFixDecision(gap.Severity, rootBead.Priority)
		if !spawn {
			log.Printf("[PRDValidation] advisory gap (warning, no fix bead): %s", gap.Description)
			continue
		}
		fixPrefix := "Fix: " + gap.Title
		if hasOpenBeadWithPrefix(existing, fixPrefix) {
			log.Printf("[PRDValidation] dedup: open fix bead already covers gap %q, skipping", gap.Title)
			continue
		}
		fixTitle := fmt.Sprintf("Fix: %s (validation round %d)", gap.Title, retryCount+1)
		fixDesc := fmt.Sprintf("PRD validation gap: %s\n\nRoot bead: %s", gap.Description, rootBeadID)

		req := types.CreateBeadRequest{
			Title:        fixTitle,
			Description:  fixDesc,
			Type:         "fix",
			Priority:     fixPriority,
			Dependencies: []types.BeadID{},
			Labels:       []string{"auto-fix", fmt.Sprintf("prd-validate-round-%d", retryCount+1)},
		}

		fixBeadID, err := v.beadsClient.CreateBead(projectPath, req)
		if err != nil {
			log.Printf("[PRDValidation] Failed to create fix bead for chain %s: %v", rootBeadID, err)
			continue
		}
		fixBeadIDs = append(fixBeadIDs, fixBeadID)
		existing = recordCreatedFix(existing, fixTitle)
		log.Printf("[PRDValidation] Created fix bead %s for gap in chain %s", fixBeadID, rootBeadID)
	}

	if len(fixBeadIDs) > 0 {
		validateTitle := fmt.Sprintf("Re-validate: %s (round %d)", rootBead.Title, retryCount+1)
		validateDesc := fmt.Sprintf("Re-validate chain against PRD after fixes\n\nRoot bead: %s", rootBeadID)

		_, validatePriority := validationFixDecision(validation.SeverityError, rootBead.Priority)
		req := types.CreateBeadRequest{
			Title:        validateTitle,
			Description:  validateDesc,
			Type:         "validation",
			Priority:     validatePriority,
			Dependencies: fixBeadIDs,
			Labels:       []string{"auto-validate", fmt.Sprintf("prd-validate-round-%d", retryCount+1)},
		}

		validateBeadID, err := v.beadsClient.CreateBead(projectPath, req)
		if err != nil {
			log.Printf("[PRDValidation] Failed to create re-validation bead for chain %s: %v", rootBeadID, err)
		} else {
			log.Printf("[PRDValidation] Created re-validation bead %s for chain %s", validateBeadID, rootBeadID)
		}
	}

	v.retryManager.IncrementValidationRetry(string(rootBeadID))
	return nil
}

// findTDDChainBeads locates RED and GREEN beads that are dependencies of a REFACTOR bead.
func (v *ValidationService) findTDDChainBeads(refactorBead types.Bead, projectPath string) (types.Bead, types.Bead, error) {
	var redBead, greenBead types.Bead
	var redFound, greenFound bool

	for _, depID := range refactorBead.Dependencies {
		// Use the projectPath parameter (the checkout directory that holds
		// the .beads store), NOT refactorBead.Project: the bead's Project
		// field is routinely empty for project-scoped beads (br stores it as
		// null), and GetBead with an empty path runs br show in the daemon's
		// cwd — no .beads there, every dependency silently skipped, and the
		// TDD chain validator reads "no RED or GREEN beads found" even when
		// the closed RED bead is a perfectly healthy dependency (2026-10-21
		// 8nz terminal-chain verification-fail loop, srv-data-harbour).
		bead, err := v.beadsClient.GetBead(projectPath, refactorBead.Repo, depID)
		if err != nil {
			continue
		}
		if isTDDRole(bead, "red", "test") {
			redBead = *bead
			redFound = true
		}
		if isTDDRole(bead, "green", "implement") {
			greenBead = *bead
			greenFound = true
		}
	}

	if !redFound && !greenFound {
		return types.Bead{}, types.Bead{}, fmt.Errorf("no RED or GREEN beads found as dependencies of %s", refactorBead.ID)
	}

	return redBead, greenBead, nil
}
