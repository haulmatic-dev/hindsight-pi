package core

import (
	"fmt"
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// VerdictRetainer posts verdict memories to Hindsight. It is deliberately
// fail-open: nil receiver, nil client, disabled client, nil memory, and HTTP
// errors are all silent no-ops. Memory observes the pipeline, never gates it.
type VerdictRetainer struct {
	client HindsightClient
}

// NewVerdictRetainer creates a retainer backed by the supplied Hindsight client.
// A nil client disables retention but still yields a safe no-op retainer.
func NewVerdictRetainer(client HindsightClient) *VerdictRetainer {
	return &VerdictRetainer{client: client}
}

// Retain synchronously sends a non-nil memory to Hindsight. Any error is
// logged and swallowed; it is never returned.
func (r *VerdictRetainer) Retain(mem *hindsight.ExecutionMemory) {
	if r == nil || mem == nil || r.client == nil || !r.client.IsEnabled() {
		return
	}
	if _, err := r.client.Retain(mem); err != nil {
		log.Printf("[VerdictRetainer] retain failed for bead %s: %v", mem.BeadID, err)
	}
}

// buildAdjudicationMemory converts a gap adjudication into an ExecutionMemory
// suitable for retention. Free-text verdict content lives in
// ExecutionResult.Output; structured metadata lives in BeadMetadata.Description.
// Verdict "ambiguous" returns nil because an unsure judgment is noise, not
// memory.
func buildAdjudicationMemory(projectName string, g gap, adj adjudication, fingerprint string) *hindsight.ExecutionMemory {
	if adj.Verdict == "ambiguous" {
		return nil
	}

	var beadID types.BeadID
	if g.SuggestedBead != "" {
		beadID = types.BeadID(g.SuggestedBead)
	} else {
		beadID = types.BeadID(fingerprint)
	}

	success := false
	if adj.Verdict == "false_positive" {
		success = true
	}

	return &hindsight.ExecutionMemory{
		BeadID: beadID,
		BeadMetadata: hindsight.BeadMetadata{
			Title:       "Adjudication: " + g.Category,
			Description: fmt.Sprintf("category: %s, severity: %s, verdict: %s, confidence: %.2f", g.Category, g.Severity, adj.Verdict, adj.Confidence),
			Type:        "adjudication",
			Project:     projectName,
		},
		ExecutionResult: hindsight.ExecutionOutcome{
			Success:  success,
			ExitCode: 0,
			Output: fmt.Sprintf("Category: %s\nSeverity: %s\nDescription: %s\nVerdict: %s\nConfidence: %.2f\nReasoning: %s",
				g.Category, g.Severity, g.Description, adj.Verdict, adj.Confidence, adj.Reasoning),
		},
		Files:      hindsight.FileChanges{},
		RecordedAt: time.Now(),
		Version:    hindsight.SchemaVersion,
	}
}

// buildFailedCloseMemory converts a failed bead close into an ExecutionMemory.
// It returns nil when there is no bead or no cause, because a cause-less failure
// teaches nothing.
func buildFailedCloseMemory(projectName string, bead *types.Bead, cause string, exitCode int) *hindsight.ExecutionMemory {
	if bead == nil || cause == "" {
		return nil
	}

	return &hindsight.ExecutionMemory{
		BeadID: bead.ID,
		BeadMetadata: hindsight.BeadMetadata{
			Title:       "Failed close: " + bead.Title,
			Description: fmt.Sprintf("exit_code: %d", exitCode),
			Type:        "failed-close",
			Project:     projectName,
		},
		ExecutionResult: hindsight.ExecutionOutcome{
			Success:  false,
			ExitCode: exitCode,
			Output:   cause,
		},
		Files:      hindsight.FileChanges{},
		RecordedAt: time.Now(),
		Version:    hindsight.SchemaVersion,
	}
}
