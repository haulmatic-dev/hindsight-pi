package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadFileReader reads beads from .beads/issues.jsonl files.
type BeadFileReader struct {
	projects []types.Project
}

// NewBeadFileReader creates a new bead file reader for the given projects.
func NewBeadFileReader(projects []types.Project) *BeadFileReader {
	return &BeadFileReader{projects: projects}
}

// FindByID searches all project issues files for a bead with the given ID.
func (r *BeadFileReader) FindByID(beadID string) *types.Bead {
	for _, project := range r.projects {
		issuesFile := filepath.Join(project.Path, ".beads", "issues.jsonl")
		beads, err := r.ReadFile(issuesFile)
		if err != nil {
			continue
		}
		for _, bead := range beads {
			if string(bead.ID) == beadID {
				return &bead
			}
		}
	}
	return nil
}

// ReadFile reads beads from a single .beads/issues.jsonl file.
func (r *BeadFileReader) ReadFile(filePath string) ([]types.Bead, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var beads []types.Bead
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		beads = append(beads, parseBeadFromRaw(raw))
	}

	return beads, nil
}

// parseBeadFromRaw converts a raw JSON map to a Bead.
func parseBeadFromRaw(raw map[string]interface{}) types.Bead {
	var bead types.Bead
	if id, ok := raw["id"].(string); ok {
		bead.ID = types.BeadID(id)
	}
	if title, ok := raw["title"].(string); ok {
		bead.Title = title
	}
	if desc, ok := raw["description"].(string); ok {
		bead.Description = desc
	}
	if t, ok := raw["type"].(string); ok {
		bead.Type = t
	}
	if status, ok := raw["status"].(string); ok {
		bead.Status = types.BeadStatus(status)
	}
	if project, ok := raw["project"].(string); ok {
		bead.Project = project
	}
	if repo, ok := raw["source_repo"].(string); ok {
		bead.Repo = repo
	}
	if deps, ok := raw["dependencies"].([]interface{}); ok {
		for _, dep := range deps {
			if depStr, ok := dep.(string); ok {
				bead.Dependencies = append(bead.Dependencies, types.BeadID(depStr))
			}
		}
	}
	return bead
}
