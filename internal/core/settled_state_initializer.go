package core

import (
	"log"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// SettledStateInitializer creates and configures the settled-state detection service.
type SettledStateInitializer struct {
	overseer *Overseer
}

// NewSettledStateInitializer creates a new initializer.
func NewSettledStateInitializer(overseer *Overseer) *SettledStateInitializer {
	return &SettledStateInitializer{overseer: overseer}
}

// Initialize creates and configures the settled-state service.
func (i *SettledStateInitializer) Initialize() {
	o := i.overseer
	config := o.config.Intelligence.SettledState

	o.settledStateSvc = NewSettledStateService(config, o.beadsClient, o)

	if o.config.Intelligence.Patterns.Enabled {
		extractorConfig := intelligence.DefaultPatternExtractorConfig()
		extractorConfig.Enabled = o.config.Intelligence.Patterns.Enabled
		extractorConfig.MinConfidence = o.config.Intelligence.Patterns.MinConfidence
		extractorConfig.MaxPatternsPerBead = o.config.Intelligence.Patterns.MaxPatternsPerBead
		extractorConfig.ExtractCodeSnippets = o.config.Intelligence.Patterns.ExtractCodeSnippets

		if o.config.Intelligence.Patterns.StoragePath != "" {
			extractorConfig.StoragePath = o.config.Intelligence.Patterns.StoragePath
		}

		extractor, err := intelligence.NewBeadPatternExtractor(extractorConfig)
		if err != nil {
			log.Printf("[SettledState] Failed to create pattern extractor: %v", err)
		} else {
			o.settledStateSvc.SetPatternExtractor(extractor)
			log.Printf("[SettledState] Pattern extractor initialized (storage: %s)", extractorConfig.StoragePath)
		}
	}

	hindsightClient := o.intelligenceHub.Hindsight()
	if hindsightClient != nil && hindsightClient.IsEnabled() {
		o.settledStateSvc.SetReflector(hindsightClient)
		log.Printf("[SettledState] Hindsight reflection enabled")
	}

	o.settledStateSvc.OnSettled(func(beadID types.BeadID, info *types.SettledBeadInfo) {
		log.Printf("[Overseer] Bead settled: %s (stability: %.2f)", beadID, info.StabilityScore)
	})

	log.Printf("[SettledState] Service initialized (check interval: %v, settlement delay: %v)",
		config.CheckInterval, config.SettlementDelay)
}
