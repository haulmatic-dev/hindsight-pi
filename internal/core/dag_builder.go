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
	// Provenance records bead ID -> source project path for every successful
	// listing so later resolution uses the real store even when several
	// projects share a repo name.
	Provenance *BeadProvenanceIndex
	// EdgeFetcher, when set, returns the full dependency map (dependent ->
	// blockers) for a repo in one call (bv --robot-graph). On success the
	// per-bead GetBead loop is skipped entirely, eliminating the N+1 br show
	// contention window (2026-09-25: lock contention dropped 43 gate edges).
	EdgeFetcher func(projectPath, repoName string) (map[string][]string, error)
}

// NewDAGBuilder creates a new DAG builder.
func NewDAGBuilder(errorLogger *ErrorLogger) *DAGBuilder {
	return &DAGBuilder{
		errorLogger: errorLogger,
	}
}

// depsUnknownMarker marks beads whose dependency set could not be determined.
// GetReadyNodes treats them as blocked (fail-closed): a bead with unknown
// dependencies must never schedule as ready, because the fallback list path
// carries no edges and gates silently stop binding (2026-09-25 incident).
const depsUnknownMarker = "deps_unknown"

// markDepsUnknown flags a bead as having an undetermined dependency set.
func markDepsUnknown(bead *types.Bead) {
	if bead.Metadata == nil {
		bead.Metadata = make(map[string]string)
	}
	bead.Metadata[depsUnknownMarker] = "true"
}

// toBeadIDs converts string IDs to BeadIDs.
func toBeadIDs(ids []string) []types.BeadID {
	if len(ids) == 0 {
		return nil
	}
	out := make([]types.BeadID, len(ids))
	for i, id := range ids {
		out[i] = types.BeadID(id)
	}
	return out
}

// note records the listed bead IDs against their source path.
func (b *DAGBuilder) note(path string, beads []types.Bead) {
	if b.Provenance == nil || path == "" {
		return
	}
	ids := make([]string, 0, len(beads))
	for _, bead := range beads {
		ids = append(ids, string(bead.ID))
	}
	b.Provenance.Note(path, ids)
}

// Build fetches all beads from projects and builds the DAG.
// completedBeads contains bead IDs that were closed in previous ticks — they are
// injected as ghost nodes so that dependency edges from still-open children remain valid.
func (b *DAGBuilder) Build(dag *DAG, beadsClient BeadsClient, projects []types.Project, completedBeads map[string]bool) error {
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
			b.note(project.Path, beads)

			if b.EdgeFetcher != nil {
				if deps, err := b.EdgeFetcher(project.Path, ""); err == nil {
					for i := range beads {
						beads[i].Dependencies = toBeadIDs(deps[string(beads[i].ID)])
					}
					allBeads = append(allBeads, beads...)
					continue
				}
				log.Printf("[DAG] EdgeFetcher failed for %s, falling back to per-bead fetch", project.Name)
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
						fmt.Sprintf("marking deps unknown for %s", bead.ID), fetchErr)
					markDepsUnknown(&bead)
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
				sourcePath := resolveRepoPath(project, repo)
				b.note(sourcePath, beads)

				if b.EdgeFetcher != nil {
					if deps, err := b.EdgeFetcher(project.Path, repo.Name); err == nil {
						for i := range beads {
							beads[i].Dependencies = toBeadIDs(deps[string(beads[i].ID)])
						}
						allBeads = append(allBeads, beads...)
						continue
					}
					log.Printf("[DAG] EdgeFetcher failed for %s/%s, falling back to per-bead fetch", project.Name, repo.Name)
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
							fmt.Sprintf("marking deps unknown for %s", bead.ID), fetchErr)
						markDepsUnknown(&bead)
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

	// Inject ghost nodes for completed beads so their children's dependency edges don't break.
	// br list drops closed beads, but issues.jsonl / the DAG still need them as edge targets.
	for beadID := range completedBeads {
		if !dag.HasNode(beadID) {
			_ = dag.AddNode(types.Bead{ID: types.BeadID(beadID), Status: types.StatusClosed})
		}
	}

	edgeErrors := 0
	for _, bead := range allBeads {
		beadID := string(bead.ID)
		for _, depID := range bead.Dependencies {
			depIDStr := string(depID)
			// Skip edges to already-completed dependencies — their dependents
			// should be treated as unblocked (InDegree stays 0).
			if completedBeads[depIDStr] {
				continue
			}
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
		return &BeadFetchError{Op: "fetch all", Err: fmt.Errorf("%d fetch errors", len(fetchErrors))}
	}

	return nil
}
