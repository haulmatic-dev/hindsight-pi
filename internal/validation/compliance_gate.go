// Package validation: compliance_gate.go
//
// Compliance-gate beads (labels: compliance-gate, prd-contract, blocks-release)
// exist to enforce that a project's PRDs are actually satisfied before
// release. The legacy implementation was descriptive text in the bead
// body: "if any gap is found, create a gap bead". The descriptive text
// was regularly skipped, leaving compliance gates closed as `failed` with
// zero gap beads created (see the 2026-06-22 srv-file-handler audit:
// COMPLIANCE GATE bead closed twice as `failed` with zero children).
//
// This file makes the gate programmatic. When a bead carries a
// compliance-gate label, the validator:
//
//  1. Inspects every direct dependency (upstream bead).
//  2. For each dependency with target_files, runs the stub detector
//     against those files. A non-clean result produces a
//     `compliance-gap` error on the gate.
//  3. Inspects every dependent (downstream bead) that was closed while
//     having `close_reason=failed`. Those need a fix bead.
//  4. Verifies the gate itself has target_files (the gate owns a tool
//     like `tools/verify-exports.go`; without one, it can't run).
//
// The result is that the gate CANNOT close until the upstream chain is
// clean. Operators don't need to trust the agent's self-report.

package validation

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ComplianceGateValidator runs compliance-gate specific checks on top
// of the standard static + stub detection.
type ComplianceGateValidator struct {
	staticChecker *StaticChecker
}

// NewComplianceGateValidator returns a fresh validator.
func NewComplianceGateValidator() *ComplianceGateValidator {
	return &ComplianceGateValidator{staticChecker: NewStaticChecker()}
}

// IsComplianceGate reports whether a bead should be validated as a
// compliance gate. The label set matches what the task-breakdown runner
// emits for the COMPLIANCE GATE bead.
func IsComplianceGate(bead types.Bead) bool {
	for _, l := range bead.Labels {
		switch l {
		case "compliance-gate", "prd-contract", "blocks-release":
			return true
		}
	}
	return false
}

// ComplianceGap describes a single gap found by the compliance gate.
// The gate can either close with a list of gaps in its description
// (legacy behaviour) or programmatically block the close (new
// behaviour).
type ComplianceGap struct {
	Type        string // "stub", "missing-file", "failed-closure"
	Description string
	AffectedBead string
	AffectedFile string
}

// ValidateGate runs the compliance-gate specific checks against a bead
// and its direct dependencies. Returns the list of gaps found.
//
// The function reads upstream beads via the provided client. The
// client is typed as a small interface so it can be backed by the
// real beads client or a mock in tests.
type GateBeadClient interface {
	GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error)
}

// ValidateGate checks the gate's target files (typically the
// `tools/verify-exports.go` style tool) plus every dependency for stub
// patterns.
func (c *ComplianceGateValidator) ValidateGate(
	ctx context.Context,
	gate types.Bead,
	deps []types.Bead,
	projectPath string,
) ([]ComplianceGap, error) {
	gaps := make([]ComplianceGap, 0)

	// 1. The gate itself must own target files; otherwise it cannot
	// actually run any verification.
	if gate.Metadata == nil || gate.Metadata["target_files"] == "" {
		gaps = append(gaps, ComplianceGap{
			Type:        "missing-file",
			Description: "compliance gate has no target_files metadata; cannot run verification",
			AffectedBead: gate.ID.String(),
		})
	}

	// 2. Every dependency with target_files must pass stub detection.
	for _, dep := range deps {
		files := extractTargetFiles(dep)
		if len(files) == 0 {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f, ".go") {
				continue
			}
			// Resolve relative target_files against projectPath so
			// DetectStubs can find the file. DetectStubs calls
			// go/parser which expects an absolute or CWD-resolvable path.
			target := f
			if projectPath != "" && !filepath.IsAbs(target) {
				target = filepath.Join(projectPath, target)
			}
			stubs := DetectStubs(target)
			for _, stub := range stubs {
				gaps = append(gaps, ComplianceGap{
					Type:         "stub",
					Description:  fmt.Sprintf("upstream dependency %s has stub (%s) in %s", dep.ID, stub.Category, f),
					AffectedBead: dep.ID.String(),
					AffectedFile: f,
				})
			}
		}
	}

	// 3. Every dependency that closed as `failed` must have a fix
	// bead (a bead that depends on it and is still open or in-progress).
	for _, dep := range deps {
		if dep.Metadata != nil && dep.Metadata["close_reason"] == "failed" {
			gaps = append(gaps, ComplianceGap{
				Type:         "failed-closure",
				Description:  fmt.Sprintf("upstream dependency %s closed as `failed` without a fix bead", dep.ID),
				AffectedBead: dep.ID.String(),
			})
		}
	}

	return gaps, nil
}

// FormatGaps renders a list of gaps in human-readable form. Used both
// in the validation error message and in the gate's bead description
// when the gap list is appended for the next iteration.
func FormatGaps(gaps []ComplianceGap) string {
	if len(gaps) == 0 {
		return "no gaps"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d gap(s) found:\n", len(gaps)))
	for i, g := range gaps {
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, g.Type, g.Description))
	}
	return sb.String()
}