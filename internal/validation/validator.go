// Package validation provides automatic validation of bead implementations
// to prevent phantom completions and ensure quality standards.
package validation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Validator orchestrates validation checks for bead implementations
type Validator struct {
	staticChecker *StaticChecker
	agentVerifier *AgentVerifier
	config        ValidationConfig
}

// ValidationConfig contains configuration for the validator
type ValidationConfig struct {
	Enabled                  bool
	StaticChecksEnabled      bool
	AgentVerificationEnabled bool
	MinCoverage              float64
	Timeout                  time.Duration
	AgentEndpoint            string
}

// DefaultValidationConfig returns default configuration
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		Enabled:                  true,
		StaticChecksEnabled:      true,
		AgentVerificationEnabled: true,
		MinCoverage:              80.0,
		Timeout:                  30 * time.Second,
		AgentEndpoint:            "http://localhost:8080/verify",
	}
}

// NewValidator creates a new validator with the given configuration
func NewValidator(config ValidationConfig) *Validator {
	return &Validator{
		staticChecker: NewStaticChecker(),
		agentVerifier: NewAgentVerifier(config.AgentEndpoint, config.Timeout),
		config:        config,
	}
}

// ValidationResult represents the outcome of validation checks
type ValidationResult struct {
	Passed       bool
	Phase        string
	Errors       []ValidationError
	Warnings     []string
	Duration     time.Duration
	StaticPassed bool
	AgentPassed  *bool // nil if agent verification not run
}

// ValidationError represents a specific validation failure
type ValidationError struct {
	Severity ErrorSeverity
	Message  string
	File     string
	Line     int
	Category string
}

// ErrorSeverity levels
type ErrorSeverity int

const (
	SeverityInfo ErrorSeverity = iota
	SeverityWarning
	SeverityError
	SeverityCritical
)

func (s ErrorSeverity) String() string {
	switch s {
	case SeverityInfo:
		return "INFO"
	case SeverityWarning:
		return "WARNING"
	case SeverityError:
		return "ERROR"
	case SeverityCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// IsEndOfChain determines if this bead is at a validation point (i.e.,
// it owns target files that should be checked before closure).
//
// Historically this function excluded TDD phase beads (tdd-red,
// tdd-green, tdd-refactor) on the assumption that the final
// "implementation" bead would catch issues. In practice this meant
// GREEN beads — the most likely place to land a stub — bypassed
// validation entirely. See docs/operations/validation-stub-detection.md
// for the post-mortem.
//
// The new contract: any bead that owns target files (i.e., anything
// the worker would write code for) is a validation point. The TDD
// phase determines which specific checks the validator runs (see
// tddPhaseChecks), but no phase is exempt from at least the static
// checks (file existence, panic stubs, empty bodies, placeholder
// comments).
func IsEndOfChain(bead types.Bead) bool {
	// Must have target files
	hasTargetFiles := bead.Metadata != nil && bead.Metadata["target_files"] != ""

	// Check if it's a terminal status (being closed) or actively
	// being worked on (so the chain can be validated mid-flight).
	isTerminalStatus := bead.Status.IsTerminal() ||
		bead.Status == types.StatusOpen ||
		bead.Status == types.StatusInProgress

	return hasTargetFiles && isTerminalStatus
}

// TDDPhase returns the TDD phase label associated with a bead, or
// empty string if the bead is not part of a TDD chain. Used by the
// validator to decide which phase-specific checks to run.
func TDDPhase(bead types.Bead) string {
	for _, l := range bead.Labels {
		switch l {
		case "tdd-red":
			return "red"
		case "tdd-green":
			return "green"
		case "tdd-refactor":
			return "refactor"
		case "tdd-syntax":
			return "syntax"
		}
	}
	return ""
}

// Validate performs full validation on a bead
func (v *Validator) Validate(ctx context.Context, bead types.Bead, projectPath string) (*ValidationResult, error) {
	if !v.config.Enabled {
		return &ValidationResult{Passed: true, Phase: "skipped"}, nil
	}

	start := time.Now()
	result := &ValidationResult{
		Passed:   true,
		Phase:    "validating",
		Errors:   make([]ValidationError, 0),
		Warnings: make([]string, 0),
	}

	// Phase 1: Static Checks (always run)
	if v.config.StaticChecksEnabled {
		staticResult := v.staticChecker.Validate(ctx, bead, projectPath)
		result.StaticPassed = staticResult.Passed
		result.Errors = append(result.Errors, staticResult.Errors...)
		result.Warnings = append(result.Warnings, staticResult.Warnings...)

		if !staticResult.Passed {
			result.Passed = false
			result.Duration = time.Since(start)
			return result, nil
		}
	}

	// Phase 2: Agent Verification (for complex beads)
	if v.config.AgentVerificationEnabled && shouldUseAgentVerification(bead) {
		agentResult, err := v.agentVerifier.Verify(ctx, bead, projectPath)
		if err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Severity: SeverityError,
				Message:  fmt.Sprintf("Agent verification failed: %v", err),
				Category: "agent",
			})
			result.Passed = false
		} else {
			result.AgentPassed = &agentResult.Passed
			result.Errors = append(result.Errors, agentResult.Errors...)
			if !agentResult.Passed {
				result.Passed = false
			}
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// ValidateQuick runs only fast static checks
func (v *Validator) ValidateQuick(ctx context.Context, bead types.Bead, projectPath string) (*ValidationResult, error) {
	if !v.config.Enabled || !v.config.StaticChecksEnabled {
		return &ValidationResult{Passed: true, Phase: "skipped"}, nil
	}

	start := time.Now()
	staticResult := v.staticChecker.Validate(ctx, bead, projectPath)
	staticResult.Duration = time.Since(start)

	return staticResult, nil
}

// shouldUseAgentVerification determines if agent verification is needed
func shouldUseAgentVerification(bead types.Bead) bool {
	// Use agent for complex features
	complexLabels := []string{"architecture", "security", "performance", "consensus"}
	for _, label := range bead.Labels {
		for _, complex := range complexLabels {
			if strings.Contains(strings.ToLower(label), complex) {
				return true
			}
		}
	}

	// Use agent for large implementations
	if files, ok := bead.Metadata["target_files"]; ok {
		fileList := strings.Split(files, ",")
		if len(fileList) > 3 {
			return true
		}
	}

	// Default: skip agent for simple beads
	return false
}

// containsLabel checks if a label exists in the list
func containsLabel(labels []string, target string) bool {
	for _, label := range labels {
		if strings.EqualFold(label, target) {
			return true
		}
	}
	return false
}

// FormatResult returns a human-readable validation report
func (r *ValidationResult) FormatResult() string {
	var sb strings.Builder

	if r.Passed {
		sb.WriteString("✅ VALIDATION PASSED\n")
	} else {
		sb.WriteString("❌ VALIDATION FAILED\n")
	}

	sb.WriteString(fmt.Sprintf("Duration: %v\n", r.Duration))
	sb.WriteString(fmt.Sprintf("Static Checks: %v\n", r.StaticPassed))
	if r.AgentPassed != nil {
		sb.WriteString(fmt.Sprintf("Agent Verification: %v\n", *r.AgentPassed))
	}

	if len(r.Errors) > 0 {
		sb.WriteString("\nErrors:\n")
		for _, err := range r.Errors {
			sb.WriteString(fmt.Sprintf("  [%s] %s", err.Severity, err.Message))
			if err.File != "" {
				sb.WriteString(fmt.Sprintf(" (%s", err.File))
				if err.Line > 0 {
					sb.WriteString(fmt.Sprintf(":%d", err.Line))
				}
				sb.WriteString(")")
			}
			sb.WriteString("\n")
		}
	}

	if len(r.Warnings) > 0 {
		sb.WriteString("\nWarnings:\n")
		for _, warn := range r.Warnings {
			sb.WriteString(fmt.Sprintf("  ⚠️  %s\n", warn))
		}
	}

	return sb.String()
}
