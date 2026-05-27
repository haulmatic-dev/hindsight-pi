package core

import (
	"os"
	"path/filepath"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ProjectResolver resolves project and repo paths for beads.
type ProjectResolver struct {
	projects []types.Project
}

// NewProjectResolver creates a new project resolver.
func NewProjectResolver(projects []types.Project) *ProjectResolver {
	return &ProjectResolver{projects: projects}
}

// ResolveProjectPath finds the project path for a given bead.
func (r *ProjectResolver) ResolveProjectPath(bead *types.Bead) string {
	for _, project := range r.projects {
		if project.Name == bead.Project {
			if bead.Repo != "" && project.Type == types.RepoTypeMulti {
				return filepath.Join(project.Path, bead.Repo)
			}
			return project.Path
		}
	}
	return ""
}

// ResolveProjectPathByWorker determines the project path using multiple strategies.
func (r *ProjectResolver) ResolveProjectPathByWorker(worker *types.Worker, projectName string) string {
	// Priority 1: Use path stored in worker record
	if worker.ProjectPath != "" {
		return worker.ProjectPath
	}

	// Priority 2: Find by project name in discovered projects
	for _, p := range r.projects {
		if p.Name == projectName {
			return p.Path
		}
	}

	// Priority 3: Use first project with valid .beads directory
	for _, proj := range r.projects {
		if HasBeadsDirectory(proj.Path) {
			return proj.Path
		}
	}

	return ""
}

// ResolveRepoPath finds the repository path for a given project and repo.
func (r *ProjectResolver) ResolveRepoPath(projectName, repoName, projectPath string) string {
	for _, p := range r.projects {
		if p.Name == projectName {
			if p.Type == "multi" {
				for _, repo := range p.Repos {
					if repo.Name == repoName {
						return repo.Path
					}
				}
			}
			return p.Path
		}
	}
	return projectPath
}

// HasBeadsDirectory checks if a path contains a .beads directory.
func HasBeadsDirectory(projectPath string) bool {
	beadsDir := filepath.Join(projectPath, ".beads")
	_, err := os.Stat(beadsDir)
	return err == nil
}
