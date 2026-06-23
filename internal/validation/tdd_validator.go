package validation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TDDGap represents a gap found during TDD validation
type TDDGap struct {
	Type        string // "red" or "green"
	Description string
	Severity    ErrorSeverity
}

// TDDValidationResult represents the outcome of TDD chain validation
type TDDValidationResult struct {
	Passed    bool
	RedPassed bool
	GreenPassed bool
	Gaps      []TDDGap
}

// TDDValidator validates TDD chain beads (RED, GREEN, REFACTOR)
type TDDValidator struct {
	staticChecker *StaticChecker
	agentVerifier *AgentVerifier
	config        ValidationConfig
}

// NewTDDValidator creates a new TDD validator
func NewTDDValidator(config ValidationConfig) *TDDValidator {
	return &TDDValidator{
		staticChecker: NewStaticChecker(),
		agentVerifier: NewAgentVerifier(config.AgentEndpoint, config.Timeout),
		config:        config,
	}
}

// ValidateTDDChain validates that RED and GREEN beads were sufficiently implemented
func (v *TDDValidator) ValidateTDDChain(redBead, greenBead, refactorBead types.Bead, projectPath string) (*TDDValidationResult, error) {
	result := &TDDValidationResult{
		Passed:      true,
		RedPassed:   true,
		GreenPassed: true,
		Gaps:        make([]TDDGap, 0),
	}

	// Check RED bead: tests exist, compile, and have meaningful assertions
	redGaps := v.validateRedBead(redBead, projectPath)
	result.Gaps = append(result.Gaps, redGaps...)
	if len(redGaps) > 0 {
		result.RedPassed = false
		result.Passed = false
	}

	// Check GREEN bead: implementation exists and matches test expectations
	greenGaps := v.validateGreenBead(greenBead, projectPath)
	result.Gaps = append(result.Gaps, greenGaps...)
	if len(greenGaps) > 0 {
		result.GreenPassed = false
		result.Passed = false
	}

	// If agent verification is enabled, run deeper checks
	if v.config.AgentVerificationEnabled && v.agentVerifier != nil {
		agentResult, err := v.runAgentVerification(redBead, greenBead, projectPath)
		if err == nil && agentResult != nil {
			for _, gap := range agentResult {
				result.Gaps = append(result.Gaps, gap)
				result.Passed = false
				if gap.Type == "red" {
					result.RedPassed = false
				} else if gap.Type == "green" {
					result.GreenPassed = false
				}
			}
		}
	}

	return result, nil
}

func (v *TDDValidator) validateRedBead(bead types.Bead, projectPath string) []TDDGap {
	gaps := make([]TDDGap, 0)

	// Extract target files from metadata
	testFiles := v.extractTargetFiles(bead, projectPath)
	if len(testFiles) == 0 {
		gaps = append(gaps, TDDGap{
			Type:        "red",
			Description: fmt.Sprintf("RED bead %s has no test files (target_files metadata missing)", bead.ID),
			Severity:    SeverityError,
		})
		return gaps
	}

	// Check each test file exists and is non-empty
	for _, tf := range testFiles {
		info, err := os.Stat(tf)
		if err != nil {
			gaps = append(gaps, TDDGap{
				Type:        "red",
				Description: fmt.Sprintf("Test file not found: %s", tf),
				Severity:    SeverityError,
			})
			continue
		}
		if info.Size() == 0 {
			gaps = append(gaps, TDDGap{
				Type:        "red",
				Description: fmt.Sprintf("Test file is empty: %s", tf),
				Severity:    SeverityError,
			})
			continue
		}

		// Check file has meaningful content (not just placeholder)
		content, err := os.ReadFile(tf)
		if err != nil {
			continue
		}
		contentStr := string(content)
		if !v.hasMeaningfulAssertions(contentStr) {
			gaps = append(gaps, TDDGap{
				Type:        "red",
				Description: fmt.Sprintf("Test file lacks meaningful assertions: %s", tf),
				Severity:    SeverityWarning,
			})
		}
	}

	return gaps
}

func (v *TDDValidator) validateGreenBead(bead types.Bead, projectPath string) []TDDGap {
	gaps := make([]TDDGap, 0)

	// Extract target files from metadata
	implFiles := v.extractTargetFiles(bead, projectPath)
	if len(implFiles) == 0 {
		gaps = append(gaps, TDDGap{
			Type:        "green",
			Description: fmt.Sprintf("GREEN bead %s has no implementation files (target_files metadata missing)", bead.ID),
			Severity:    SeverityError,
		})
		return gaps
	}

	// Check each implementation file exists and is non-empty
	for _, f := range implFiles {
		info, err := os.Stat(f)
		if err != nil {
			gaps = append(gaps, TDDGap{
				Type:        "green",
				Description: fmt.Sprintf("Implementation file not found: %s", f),
				Severity:    SeverityError,
			})
			continue
		}
		if info.Size() == 0 {
			gaps = append(gaps, TDDGap{
				Type:        "green",
				Description: fmt.Sprintf("Implementation file is empty: %s", f),
				Severity:    SeverityError,
			})
			continue
		}
		// Stub detection beyond "is the file empty?". The static
		// checker also runs DetectStubs, but we surface the gaps here
		// as green-bead gaps so the validation service's fix-bead
		// creation flow picks them up. The check is duplicated
		// intentionally: a static-check failure should produce a fix
		// bead regardless of how it was discovered.
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
			stubs := DetectStubs(f)
			for _, stub := range stubs {
				gaps = append(gaps, TDDGap{
					Type:        "green",
					Description: fmt.Sprintf("Stub pattern (%s) in %s: %s", stub.Category, stub.Function, stub.Detail),
					Severity:    SeverityError,
				})
			}
		}
	}

	return gaps
}

func (v *TDDValidator) runAgentVerification(redBead, greenBead types.Bead, projectPath string) ([]TDDGap, error) {
	// Agent verification is optional and requires an endpoint
	if v.agentVerifier == nil || v.agentVerifier.endpoint == "" {
		return nil, nil
	}

	ctx := context.Background()
	result, err := v.agentVerifier.Verify(ctx, greenBead, projectPath)
	if err != nil {
		return nil, err
	}

	gaps := make([]TDDGap, 0)
	if !result.Passed {
		for _, issue := range result.Errors {
			gapType := "green"
			if strings.Contains(issue.Message, "test") || strings.Contains(issue.Message, "assert") {
				gapType = "red"
			}
			gaps = append(gaps, TDDGap{
				Type:        gapType,
				Description: issue.Message,
				Severity:    issue.Severity,
			})
		}
	}

	return gaps, nil
}

func (v *TDDValidator) extractTargetFiles(bead types.Bead, projectPath string) []string {
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

func (v *TDDValidator) hasMeaningfulAssertions(content string) bool {
	// Simple heuristic: check for common assertion patterns
	assertionPatterns := []string{
		"assert", "require", "expect", "t.Run", "Test",
	}
	lowerContent := strings.ToLower(content)
	for _, pattern := range assertionPatterns {
		if strings.Contains(lowerContent, pattern) {
			return true
		}
	}
	return false
}
