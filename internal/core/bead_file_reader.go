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
	// Dependencies arrive in more than one shape and only one of them was
	// handled, silently. `br` writes issues.jsonl entries as OBJECTS:
	//
	//   "dependencies": [{"issue_id":"a","depends_on_id":"b","type":"blocks"}]
	//
	// while this only accepted plain strings, so `dep.(string)` failed on every
	// element and Dependencies came back empty for every bead. Nothing errored.
	//
	// The cost was the entire chain layer. ChainRegistrar treats a bead with no
	// dependencies as a chain root, so on 2026-08-10 it logged "Found 1450 root
	// beads" and "Registered 0 chains from 1450 total beads" — and with zero
	// chains registered the completion callback can never fire. Everything
	// hanging off it has therefore never run in production: A1 quarantine of
	// failed chains, IntegrateAndRemove for clean ones, RunPRDValidation, and
	// ExtractPatterns. `Chain completed:` has literally never appeared in a log.
	//
	// `depends_on_id` is the direction to follow: the record lives on the
	// depending bead (issue_id == the owning bead in 12/12 records checked) and
	// points at what it waits for — a `Fix: X` bead depends_on `X`.
	if deps, ok := raw["dependencies"].([]interface{}); ok {
		for _, dep := range deps {
			switch d := dep.(type) {
			case string:
				// Legacy/simple shape, still accepted.
				if d != "" {
					bead.Dependencies = append(bead.Dependencies, types.BeadID(d))
				}
			case map[string]interface{}:
				// `depends_on_id` is issues.jsonl; `id` is what `br show --json`
				// emits for the same relationship. Take whichever is present so
				// one parser covers both files and command output.
				for _, key := range []string{"depends_on_id", "id"} {
					if s, ok := d[key].(string); ok && s != "" {
						bead.Dependencies = append(bead.Dependencies, types.BeadID(s))
						break
					}
				}
			}
		}
	}
	// Labels carry durable close verdicts (closed-failed, fix-recursion-capped);
	// without parsing them the chain integrate-vs-quarantine decision is blind.
	if labels, ok := raw["labels"].([]interface{}); ok {
		for _, l := range labels {
			if ls, ok := l.(string); ok {
				bead.Labels = append(bead.Labels, ls)
			}
		}
	}
	return bead
}
