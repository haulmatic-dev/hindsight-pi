package core

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

type chainCompletionRegistrar interface {
	RegisterChain(rootBeadID types.BeadID, allBeadIDs []types.BeadID, projectPath string)
}

type beadFileReader interface {
	ReadFile(filePath string) ([]types.Bead, error)
}

// ChainRegistrar registers bead chains from issues.jsonl files.
type ChainRegistrar struct {
	chainCompletionDetector chainCompletionRegistrar
	beadFileReader          beadFileReader
}

// NewChainRegistrar creates a new chain registrar.
func NewChainRegistrar(detector chainCompletionRegistrar, reader beadFileReader) *ChainRegistrar {
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

	// Find roots (beads with no dependencies). Human gates (approval-gate,
	// approval) are never chain roots: chains must root at the epic/PRD.
	// When a gate is the dependency root, descend to the first non-gate
	// descendant that itself has dependents and use those as chain roots.
	// This prevents gate-rooted chains where the gate — not the epic — becomes
	// the chain key and confuses the reconciler.
	isGate := func(b types.Bead) bool {
		t := strings.ToLower(strings.TrimSpace(b.Type))
		return t == "approval-gate" || t == "approval"
	}

	roots := make([]types.Bead, 0)
	queued := make(map[string]bool)
	for _, bead := range allBeadMap {
		if len(bead.Dependencies) != 0 {
			continue
		}
		if isGate(bead) {
			var descend func(beadID string)
			descend = func(beadID string) {
				for _, depID := range dependsOnMap[beadID] {
					if queued[depID] {
						continue
					}
					child := allBeadMap[depID]
					if isGate(child) {
						descend(depID)
						continue
					}
					if len(dependsOnMap[depID]) > 0 {
						queued[depID] = true
						roots = append(roots, child)
					}
				}
			}
			descend(string(bead.ID))
			continue
		}
		id := string(bead.ID)
		if !queued[id] {
			queued[id] = true
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
