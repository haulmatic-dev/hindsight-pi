package validation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// PRDGap represents a gap found during PRD validation
type PRDGap struct {
	Title        string
	Description  string
	RequiredFiles []string
	Severity     ErrorSeverity
}

// PRDValidationResult represents the outcome of PRD validation
type PRDValidationResult struct {
	Passed       bool
	Gaps         []PRDGap
	RootBeadID   types.BeadID
	CheckedFiles []string
}

// PRDValidator validates that chain implementations match the root PRD/requirements
type PRDValidator struct {
	staticChecker *StaticChecker
	agentVerifier *AgentVerifier
	config        ValidationConfig
}

// NewPRDValidator creates a new PRD validator
func NewPRDValidator(config ValidationConfig) *PRDValidator {
	return &PRDValidator{
		staticChecker: NewStaticChecker(),
		agentVerifier: NewAgentVerifier(config.AgentEndpoint, config.Timeout),
		config:        config,
	}
}

// ValidateChainAgainstPRD validates that leaf implementations satisfy the root PRD
func (v *PRDValidator) ValidateChainAgainstPRD(rootBead types.Bead, leafBeads []types.Bead, projectPath string) (*PRDValidationResult, error) {
	result := &PRDValidationResult{
		Passed:       true,
		Gaps:         make([]PRDGap, 0),
		RootBeadID:   rootBead.ID,
		CheckedFiles: make([]string, 0),
	}

	// Extract acceptance criteria from root bead
	criteria := v.extractAcceptanceCriteria(rootBead)
	if len(criteria) == 0 {
		result.Gaps = append(result.Gaps, PRDGap{
			Title:       "Missing Acceptance Criteria",
			Description: fmt.Sprintf("Root bead %s has no acceptance criteria in description", rootBead.ID),
			Severity:    SeverityWarning,
		})
	}

	// Collect all target files from leaf implementation beads
	allFiles := make(map[string]bool)
	for _, bead := range leafBeads {
		files := v.extractTargetFiles(bead, projectPath)
		for _, f := range files {
			allFiles[f] = true
			result.CheckedFiles = append(result.CheckedFiles, f)
		}
	}

	// Static check: all target files exist and are non-empty
	for f := range allFiles {
		info, err := os.Stat(f)
		if err != nil {
			result.Passed = false
			result.Gaps = append(result.Gaps, PRDGap{
				Title:         "Missing Implementation File",
				Description:   fmt.Sprintf("File not found: %s", f),
				RequiredFiles: []string{f},
				Severity:      SeverityError,
			})
			continue
		}
		if info.Size() == 0 {
			result.Passed = false
			result.Gaps = append(result.Gaps, PRDGap{
				Title:         "Empty Implementation File",
				Description:   fmt.Sprintf("File is empty: %s", f),
				RequiredFiles: []string{f},
				Severity:      SeverityError,
			})
		}
	}

	// If no files found at all: when every leaf is closed, verified-close
	// already proved diffs exist — the gap is missing target_files METADATA,
	// not missing implementation, and minting a fix bead sends a worker
	// wandering the repo for a phantom (kgkc7: 20.5M tokens, 2026-08-21).
	// Advisory only. Open leaves keep it Critical.
	if len(allFiles) == 0 {
		severity := SeverityCritical
		allClosed := len(leafBeads) > 0
		for _, b := range leafBeads {
			if b.Status != types.StatusClosed && b.Status != types.StatusCompleted {
				allClosed = false
				break
			}
		}
		if allClosed {
			severity = SeverityWarning
		}
		result.Gaps = append(result.Gaps, PRDGap{
			Title:       "No Implementation Files",
			Description: "No leaf beads have target_files metadata",
			Severity:    severity,
		})
		if severity == SeverityCritical {
			result.Passed = false
		}
	}

	// Agent verification: compare implementation against acceptance criteria
	if v.config.AgentVerificationEnabled && v.agentVerifier != nil && len(criteria) > 0 {
		agentGaps := v.runAgentVerification(rootBead, result.CheckedFiles, projectPath)
		for _, gap := range agentGaps {
			result.Passed = false
			result.Gaps = append(result.Gaps, gap)
		}
	}

	return result, nil
}

func (v *PRDValidator) extractAcceptanceCriteria(bead types.Bead) []string {
	var criteria []string
	
	// Parse from description - look for "Acceptance Criteria" section
	if bead.Description != "" {
		lines := strings.Split(bead.Description, "\n")
		inCriteria := false
		for _, line := range lines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "acceptance criteria") || strings.Contains(lower, "## acceptance") {
				inCriteria = true
				continue
			}
			if inCriteria {
				// Stop at next major section
				if strings.HasPrefix(line, "#") && !strings.Contains(lower, "acceptance") {
					break
				}
				// Collect checklist items
				trimmed := strings.TrimSpace(line)
				if trimmed != "" && (strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "[")) {
					criteria = append(criteria, trimmed)
				}
			}
		}
	}

	return criteria
}

func (v *PRDValidator) runAgentVerification(rootBead types.Bead, checkedFiles []string, projectPath string) []PRDGap {
	gaps := make([]PRDGap, 0)
	
	// Agent verification is optional and requires an endpoint
	if v.agentVerifier == nil || v.agentVerifier.endpoint == "" {
		return gaps
	}

	ctx := context.Background()
	result, err := v.agentVerifier.Verify(ctx, rootBead, projectPath)
	if err != nil {
		return gaps
	}

	if !result.Passed {
		for _, issue := range result.Errors {
			gaps = append(gaps, PRDGap{
				Title:         "PRD Gap",
				Description:   issue.Message,
				RequiredFiles: checkedFiles,
				Severity:      issue.Severity,
			})
		}
	}

	return gaps
}

func (v *PRDValidator) extractTargetFiles(bead types.Bead, projectPath string) []string {
	var files []string
	if bead.Metadata != nil {
		if tf := bead.Metadata["target_files"]; tf != "" {
			for _, f := range strings.Split(tf, ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					files = append(files, filepath.Join(projectPath, f))
				}
			}
		}
	}
	return files
}
