package core

import (
	"fmt"
	"log"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadCloser closes beads via beadsClient based on worker completion status.
type BeadCloser struct {
	beadsClient     BeadsClient
	intelligenceHub *IntelligenceHub
	projectResolver *ProjectResolver
	overseer        *Overseer
}

// NewBeadCloser creates a new bead closer.
func NewBeadCloser(beadsClient BeadsClient, intelligenceHub *IntelligenceHub, projectResolver *ProjectResolver, overseer *Overseer) *BeadCloser {
	return &BeadCloser{
		beadsClient:     beadsClient,
		intelligenceHub: intelligenceHub,
		projectResolver: projectResolver,
		overseer:        overseer,
	}
}

// Close closes the bead via beadsClient based on worker completion status.
func (c *BeadCloser) Close(worker *types.Worker, success bool) error {
	projectName := worker.BeadID.Project()
	repoName := worker.BeadID.Repo()

	log.Printf("[closeBead] Closing bead %s (project: %s, repo: %s)",
		worker.BeadID.String(), projectName, repoName)

	projectPath := c.projectResolver.ResolveProjectPathByWorker(worker, projectName)
	if projectPath == "" {
		return fmt.Errorf("project %s not found", projectName)
	}

	reason := statusReason(success)
	log.Printf("[closeBead] Closing with reason: %s (path: %s)", reason, projectPath)

	// Queue TLDR indexing on successful completion
	if success {
		repoPath := c.projectResolver.ResolveRepoPath(projectName, repoName, projectPath)
		if repoPath != "" {
			c.intelligenceHub.QueueRepoIndexing(projectName, repoPath)
		}
	}

	bead, err := c.beadsClient.GetBead(projectPath, repoName, worker.BeadID)
	if err == nil && bead.Status.IsTerminal() {
		log.Printf("[closeBead] Bead %s already closed (status: %s), skipping", worker.BeadID.String(), bead.Status)
		return nil
	}

	// Run TDD validation for REFACTOR beads before closing
	if success && bead != nil && bead.Type == "refactor" {
		if err := c.overseer.RunTDDValidation(*bead, projectPath); err != nil {
			log.Printf("[closeBead] TDD validation error for %s: %v", worker.BeadID.String(), err)
		}
	}

	return c.beadsClient.CloseBead(projectPath, repoName, worker.BeadID, reason)
}
