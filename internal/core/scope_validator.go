package core

import (
	"fmt"
	"log"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ScopeValidator validates the scope of changes against configured limits.
type ScopeValidator struct {
	config *Config
}

// NewScopeValidator creates a new scope validator.
func NewScopeValidator(config *Config) *ScopeValidator {
	return &ScopeValidator{config: config}
}

// maxScopeFiles bounds how many files one bead may target.
//
// Previously borrowed from intelligence.tldr.max_files, which coupled a scope
// rule to a tool that has been removed — and silently disabled the check
// whenever that tool was disabled. It is a scope invariant, so it stands on its
// own now.
const maxScopeFiles = 20

// Validate validates the scope of changes against configured limits.
func (v *ScopeValidator) Validate(bundle *types.IntelligenceBundle, targetFiles []string) error {
	if len(targetFiles) > maxScopeFiles {
		return fmt.Errorf("too many target files: %d exceeds limit of %d",
			len(targetFiles), maxScopeFiles)
	}

	if bundle.ImpactAnalysis != nil {
		totalFiles := len(targetFiles) + len(bundle.ImpactAnalysis.AffectedFiles)

		if totalFiles > maxScopeFiles*2 {
			return fmt.Errorf("impact scope too large: %d total files affected exceeds safe threshold",
				totalFiles)
		}

		if bundle.ImpactAnalysis.Complexity > 100 {
			return fmt.Errorf("complexity score too high: %d (max recommended: 100)",
				bundle.ImpactAnalysis.Complexity)
		}

		if bundle.ImpactAnalysis.RiskLevel == "high" {
			log.Printf("[Intelligence] Warning: High risk level detected for change scope")
		}
	}

	return nil
}
