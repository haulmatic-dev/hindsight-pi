package core

import (
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadAnalyzer provides utility functions for analyzing bead metadata.
type BeadAnalyzer struct {
	projects []types.Project
}

// NewBeadAnalyzer creates a new bead analyzer for the given projects.
func NewBeadAnalyzer(projects []types.Project) *BeadAnalyzer {
	return &BeadAnalyzer{projects: projects}
}

// SetProjects updates the projects list.
func (ba *BeadAnalyzer) SetProjects(projects []types.Project) {
	ba.projects = projects
}

// ExtractTargetFiles extracts target file paths from bead metadata and description.
func (ba *BeadAnalyzer) ExtractTargetFiles(bead *types.Bead) []string {
	var files []string
	seen := make(map[string]bool)

	// Check metadata for explicit file list
	if bead.Metadata != nil {
		if fileList, ok := bead.Metadata["files"]; ok {
			for _, file := range strings.Split(fileList, ",") {
				file = strings.TrimSpace(file)
				if file != "" && !seen[file] {
					files = append(files, file)
					seen[file] = true
				}
			}
		}
	}

	// Parse description for file references
	desc := bead.Description + " " + bead.Title
	words := strings.Fields(desc)

	for _, word := range words {
		if strings.Contains(word, "/") || strings.Contains(word, ".go") ||
			strings.Contains(word, ".js") || strings.Contains(word, ".ts") ||
			strings.Contains(word, ".py") || strings.Contains(word, ".java") {
			cleaned := strings.Trim(word, ".,;:!?()[]{}\"")
			if cleaned != "" && !seen[cleaned] && len(cleaned) < 200 {
				files = append(files, cleaned)
				seen[cleaned] = true
			}
		}
	}

	return files
}

// FindProjectPath finds the project path for a given bead.
func (ba *BeadAnalyzer) FindProjectPath(bead *types.Bead) string {
	for _, project := range ba.projects {
		if project.Name == bead.Project {
			if bead.Repo != "" && project.Type == types.RepoTypeMulti {
				return filepath.Join(project.Path, bead.Repo)
			}
			return project.Path
		}
	}
	return ""
}
