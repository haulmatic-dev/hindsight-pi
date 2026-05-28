package core

import (
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
)

// ChainCompletionInitializer creates and configures the chain completion detector.
type ChainCompletionInitializer struct {
	overseer *Overseer
}

// NewChainCompletionInitializer creates a new initializer.
func NewChainCompletionInitializer(overseer *Overseer) *ChainCompletionInitializer {
	return &ChainCompletionInitializer{overseer: overseer}
}

// Initialize creates and configures the chain completion detector.
func (i *ChainCompletionInitializer) Initialize() {
	o := i.overseer
	config := ChainCompletionConfig{
		Enabled:           o.config.Overseer.ChainDetection.Enabled,
		DetectionInterval: o.config.Overseer.ChainDetection.Interval,
		Timeout:           o.config.Overseer.ChainDetection.Timeout,
	}

	if config.DetectionInterval == 0 {
		config.DetectionInterval = 10 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute
	}

	o.chainCompletionDetector = NewChainCompletionDetector(config, o)
	o.chainCompletionDetector.Start()

	extractor := NewChainPatternExtractor(
		o,
		intelligence.NewQualityGateService(),
		nil,
	)

	o.chainCompletionDetector.SetCallback(func(event ChainCompletedEvent) {
		log.Printf("[Overseer] Chain completed: root=%s", event.ChainInfo.RootBeadID)

		projectPath := o.findProjectPathForBeadID(event.ChainInfo.RootBeadID)
		if projectPath != "" {
			if err := o.RunPRDValidation(event.ChainInfo, projectPath); err != nil {
				log.Printf("[Overseer] PRD validation failed: %v", err)
			}
		}

		if err := extractor.ExtractPatterns(event); err != nil {
			log.Printf("[Overseer] Chain pattern extraction failed: %v", err)
		}
	})

	log.Printf("[ChainCompletion] Detector initialized (interval: %v, timeout: %v)",
		config.DetectionInterval, config.Timeout)
}
