package main

import (
	"log"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// extractPatternsAtClose stores close-time patterns derived from the worker's
// HYDRA_VERDICT. This is the compounding-knowledge sink that does not depend
// on settled_state: patterns appear immediately at bead completion instead of
// after a settlement delay, and no idle-shutdown path is involved.
//
// Fail-open by contract: disabled config, a missing verdict, an unreadable
// storage path, or an extraction error never affect bead completion — the
// pattern store is an optimisation, not a gate.
func extractPatternsAtClose(config WorkerConfig, bead types.Bead, result *ExecutionResult) {
	if !config.Patterns.Enabled || result == nil || result.Verdict == nil {
		return
	}

	cfg := intelligence.DefaultPatternExtractorConfig()
	cfg.Enabled = config.Patterns.Enabled
	if config.Patterns.StoragePath != "" {
		cfg.StoragePath = config.Patterns.StoragePath
	}
	if config.Patterns.MinConfidence > 0 {
		cfg.MinConfidence = config.Patterns.MinConfidence
	}
	if config.Patterns.MaxPatternsPerBead > 0 {
		cfg.MaxPatternsPerBead = config.Patterns.MaxPatternsPerBead
	}

	ext, err := intelligence.NewBeadPatternExtractor(cfg)
	if err != nil {
		log.Printf("[WARN] pattern extractor init failed (non-fatal): %v", err)
		return
	}

	v := result.Verdict
	if err := ext.ExtractVerdict(bead.ID, intelligence.VerdictPatternInput{
		Done:         v.Done,
		ACMet:        v.ACMet,
		ACUnmet:      v.ACUnmet,
		FilesChanged: v.FilesChanged,
		Summary:      v.Summary,
	}); err != nil {
		log.Printf("[WARN] pattern extraction failed (non-fatal): %v", err)
	}
}
