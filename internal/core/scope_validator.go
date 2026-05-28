package core

import (
	"fmt"
	"log"
)

// ScopeValidator validates the scope of changes against configured limits.
type ScopeValidator struct {
	config *Config
}

// NewScopeValidator creates a new scope validator.
func NewScopeValidator(config *Config) *ScopeValidator {
	return &ScopeValidator{config: config}
}

// Validate validates the scope of changes against configured limits.
func (v *ScopeValidator) Validate(bundle *IntelligenceBundle, targetFiles []string) error {
	config := v.config.Intelligence

	if config.TLDR.Enabled && len(targetFiles) > config.TLDR.MaxFiles {
		return fmt.Errorf("too many target files: %d exceeds limit of %d",
			len(targetFiles), config.TLDR.MaxFiles)
	}

	if bundle.ImpactAnalysis != nil {
		totalFiles := len(targetFiles) + len(bundle.ImpactAnalysis.AffectedFiles)

		if totalFiles > config.TLDR.MaxFiles*2 {
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
