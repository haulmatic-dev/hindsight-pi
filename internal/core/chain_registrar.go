package core

import (
	"log"
	"path/filepath"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ChainRegistrar registers bead chains from issues.jsonl files.
type ChainRegistrar struct {
	chainCompletionDetector *ChainCompletionDetector
	beadFileReader          *BeadFileReader
}

// NewChainRegistrar creates a new chain registrar.
func NewChainRegistrar(detector *ChainCompletionDetector, reader *BeadFileReader) *ChainRegistrar {
	return &ChainRegistrar{
		chainCompletionDetector: detector,
		beadFileReader:          reader,
	}
}

// RegisterFromProjects reads beads from all projects and registers chains.
func (r *ChainRegistrar) RegisterFromProjects(projects []types.Project) {
	if r.chainCompletionDetector == nil {
		return
	}

	allBeadMap := make(map[string]types.Bead)
	beadProjectPath := make(map[string]string)
	dependsOnMap := make(map[string][]string)

	for _, project := range projects {
		issuesFile := filepath.Join(project.Path, ".beads", "issues.jsonl")
		beads, err := r.beadFileReader.ReadFile(issuesFile)
		if err != nil {
			log.Printf("[ChainCompletion] Could not read beads from %s: %v", issuesFile, err)
			continue
		}
		log.Printf("[ChainCompletion] Read %d beads from %s", len(beads), project.Name)
		for _, bead := range beads {
			allBeadMap[string(bead.ID)] = bead
			beadProjectPath[string(bead.ID)] = project.Path
			for _, dep := range bead.Dependencies {
				depStr := string(dep)
				dependsOnMap[depStr] = append(dependsOnMap[depStr], string(bead.ID))
			}
		}
	}

	if len(allBeadMap) == 0 {
		log.Printf("[ChainCompletion] No beads found, skipping chain registration")
		return
	}

	// Find roots (beads with no dependencies)
	roots := make([]types.Bead, 0)
	for _, bead := range allBeadMap {
		if len(bead.Dependencies) == 0 {
			roots = append(roots, bead)
		}
	}

	log.Printf("[ChainCompletion] Found %d root beads", len(roots))

	chainCount := 0
	visited := make(map[string]bool)

	for _, root := range roots {
		var chainBeads []types.BeadID

		var dfs func(beadID string)
		dfs = func(beadID string) {
			if visited[beadID] {
				return
			}
			visited[beadID] = true

			bid, _ := types.ParseBeadID(beadID)
			chainBeads = append(chainBeads, bid)

			for _, dependent := range dependsOnMap[beadID] {
				dfs(dependent)
			}
		}

		dfs(string(root.ID))

		if len(chainBeads) > 1 {
			// Record the ROOT bead's project path so chain completion
			// integrates into the project the chain actually lives in —
			// never a name lookup that can match a same-named project.
			r.chainCompletionDetector.RegisterChain(chainBeads[0], chainBeads, beadProjectPath[string(root.ID)])
			chainCount++
			log.Printf("[ChainCompletion] Registered chain with %d beads starting from %s", len(chainBeads), root.ID)
		}
	}

	log.Printf("[ChainCompletion] Registered %d chains from %d total beads", chainCount, len(allBeadMap))
}
