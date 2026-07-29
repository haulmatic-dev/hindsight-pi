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

		// Per-chain worktree isolation. "Completed" only means every bead reached
		// a terminal state — a chain whose beads closed as FAILED used to
		// integrate here exactly like a successful one, which left a
		// deliberately red test on the testbed's working branch (2026-07-28).
		// Failed chains now quarantine: work preserved on hydra/quarantine/<key>
		// for the fix bead to inspect, working branch untouched.
		if o.worktreeManager != nil && projectPath != "" {
			key := event.ChainInfo.RootBeadID.String()
			if o.chainCompletionDetector != nil && o.chainCompletionDetector.ChainHasFailedBeads(event.ChainInfo.AllBeadIDs) {
				if qBranch, qErr := o.worktreeManager.QuarantineAndRemove(projectPath, key); qErr != nil {
					log.Printf("[Worktree] quarantine failed for chain %s: %v", event.ChainInfo.RootBeadID, qErr)
				} else if qBranch != "" {
					log.Printf("[Worktree] quarantined failed chain %s on %s (not integrated)", event.ChainInfo.RootBeadID, qBranch)
					if o.beadCloser != nil && o.beadCloser.events != nil {
						o.beadCloser.events.EmitRepo(projectPath, Event{Type: "chain_quarantined",
							Bead: key, Reason: "failed_beads_in_chain",
							Fields: map[string]any{"branch": qBranch}})
					}
				}
			} else if err := o.worktreeManager.IntegrateAndRemove(projectPath, key); err != nil {
				log.Printf("[Worktree] integrate failed for chain %s: %v", event.ChainInfo.RootBeadID, err)
			}
		}

		if err := extractor.ExtractPatterns(event); err != nil {
			log.Printf("[Overseer] Chain pattern extraction failed: %v", err)
		}
	})

	log.Printf("[ChainCompletion] Detector initialized (interval: %v, timeout: %v)",
		config.DetectionInterval, config.Timeout)
}
