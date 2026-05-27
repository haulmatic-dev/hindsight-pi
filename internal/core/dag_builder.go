package core

import (
	"errors"
	"fmt"
	"log"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// DAGBuilder fetches beads and builds the dependency graph.
type DAGBuilder struct {
	errorLogger *ErrorLogger
}

// NewDAGBuilder creates a new DAG builder.
func NewDAGBuilder(errorLogger *ErrorLogger) *DAGBuilder {
	return &DAGBuilder{
		errorLogger: errorLogger,
	}
}

// Build fetches all beads from projects and builds the DAG.
func (b *DAGBuilder) Build(dag *DAG, beadsClient BeadsClient, projects []types.Project) error {
	// If DAG is nil, create a new one
	if dag == nil {
		log.Printf("[DAG] DAG was nil, creating new instance")
		dag = NewDAG()
	}

	dag.Clear()

	var allBeads []types.Bead
	var fetchErrors []error

	for _, project := range projects {
		if project.Type == types.RepoTypeSingle {
			beads, err := beadsClient.ListBeads(project.Path, "")
			if err != nil {
				fetchErr := &BeadFetchError{
					Op:      "list",
					Project: project.Name,
					Err:     err,
				}
				b.errorLogger.GracefulDegradation("beadsClient", "ListBeads",
					fmt.Sprintf("skipping project %s", project.Name), fetchErr)
				fetchErrors = append(fetchErrors, fetchErr)
				continue
			}

			for _, bead := range beads {
				fullBead, err := beadsClient.GetBead(project.Path, "", bead.ID)
				if err != nil {
					fetchErr := &BeadFetchError{
						Op:      "get",
						Project: project.Name,
						Err:     err,
					}
					b.errorLogger.GracefulDegradation("beadsClient", "GetBead",
						fmt.Sprintf("using list data for %s", bead.ID), fetchErr)
					allBeads = append(allBeads, bead)
				} else {
					allBeads = append(allBeads, *fullBead)
				}
			}
		} else {
			for _, repo := range project.Repos {
				beads, err := beadsClient.ListBeads(project.Path, repo.Name)
				if err != nil {
					fetchErr := &BeadFetchError{
						Op:      "list",
						Project: project.Name,
						Repo:    repo.Name,
						Err:     err,
					}
					b.errorLogger.GracefulDegradation("beadsClient", "ListBeads",
						fmt.Sprintf("skipping repo %s/%s", project.Name, repo.Name), fetchErr)
					fetchErrors = append(fetchErrors, fetchErr)
					continue
				}

				for _, bead := range beads {
					fullBead, err := beadsClient.GetBead(project.Path, repo.Name, bead.ID)
					if err != nil {
						fetchErr := &BeadFetchError{
							Op:      "get",
							Project: project.Name,
							Repo:    repo.Name,
							Err:     err,
						}
						b.errorLogger.GracefulDegradation("beadsClient", "GetBead",
							fmt.Sprintf("using list data for %s", bead.ID), fetchErr)
						allBeads = append(allBeads, bead)
					} else {
						allBeads = append(allBeads, *fullBead)
					}
				}
			}
		}
	}

	log.Printf("[DAG] Building DAG with %d beads (from %d projects)", len(allBeads), len(projects))

	nodeErrors := 0
	for _, bead := range allBeads {
		if err := dag.AddNode(bead); err != nil {
			if errors.Is(err, ErrNodeAlreadyExists) {
				log.Printf("[DAG] Duplicate bead skipped: %s", bead.ID)
				continue
			}
			b.errorLogger.Error("DAG", "AddNode", string(bead.ID), err, "warn")
			nodeErrors++
		}
	}

	edgeErrors := 0
	for _, bead := range allBeads {
		beadID := string(bead.ID)
		for _, depID := range bead.Dependencies {
			depIDStr := string(depID)
			if err := dag.AddEdge(beadID, depIDStr); err != nil {
				switch {
				case errors.Is(err, ErrNodeNotFound):
					log.Printf("[DAG] Dependency not found: %s -> %s", depIDStr, beadID)
				case errors.Is(err, ErrSelfReference):
					log.Printf("[DAG] Self-reference detected in bead %s, skipping edge", beadID)
				case errors.Is(err, ErrEdgeCreatesCycle):
					log.Printf("[DAG] Cycle would be created: %s -> %s, skipping edge", depIDStr, beadID)
				default:
					b.errorLogger.Error("DAG", "AddEdge", beadID, err, "warn")
				}
				edgeErrors++
			}
		}
	}

	if cycle, err := dag.DetectCycle(); err != nil {
		b.errorLogger.Error("DAG", "DetectCycle", "", err, "error")
	} else if cycle != nil {
		log.Printf("[DAG] Cycle detected: %v", cycle)
	}

	stats := DAGStats{
		TotalNodes:     dag.GetNodeCount(),
		ReadyNodes:     len(dag.GetReadyNodes()),
		BlockedNodes:   dag.GetBlockedCount(),
		CompletedNodes: dag.GetCompletedCount(),
		FailedNodes:    dag.GetFailedCount(),
	}

	log.Printf("[DAG] Build complete: %+v (fetch errors: %d, node errors: %d, edge errors: %d)",
		stats, len(fetchErrors), nodeErrors, edgeErrors)

	if len(allBeads) == 0 && len(fetchErrors) > 0 {
		return fmt.Errorf("failed to fetch any beads: %d fetch errors occurred", len(fetchErrors))
	}

	return nil
}
