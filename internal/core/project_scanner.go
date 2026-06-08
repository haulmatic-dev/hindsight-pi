package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ProjectScanner discovers and inspects projects for Hydra scheduling.
type ProjectScanner struct {
	config   *Config
	projects []types.Project
}

// NewProjectScanner creates a new project scanner.
func NewProjectScanner(config *Config) *ProjectScanner {
	return &ProjectScanner{config: config}
}

// Projects returns the discovered projects.
func (ps *ProjectScanner) Projects() []types.Project {
	return ps.projects
}

// Discover finds all projects from config or auto-discovers them.
func (ps *ProjectScanner) Discover() error {
	// If projects are configured, use those
	if len(ps.config.Projects) > 0 {
		for _, pc := range ps.config.Projects {
			project := types.Project{
				Name: pc.Name,
				Path: pc.Path,
				Type: types.RepoType(pc.Type),
			}
			if pc.Type == "multi" {
				if len(pc.Repos) > 0 {
					// Use explicitly listed repos
					for _, rc := range pc.Repos {
						project.Repos = append(project.Repos, types.Repo{
							Name: rc.Name,
							Path: rc.Path,
						})
					}
				} else {
					// Auto-discover repos inside project path
					repos := ps.discoverRepos(pc.Path)
					if len(repos) > 0 {
						project.Repos = repos
					} else {
						log.Printf("⚠️  Multi project %s has no repos found in %s", pc.Name, pc.Path)
					}
				}
			}
			ps.projects = append(ps.projects, project)
		}
		return nil
	}

	// Auto-discovery
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	rootPath := filepath.Join(homeDir, "source-code")

	entries, err := os.ReadDir(rootPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("⚠️  Projects directory not found: %s", rootPath)
			return nil
		}
		return fmt.Errorf("failed to read projects directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectPath := filepath.Join(rootPath, entry.Name())
		project, err := ps.inspect(projectPath, entry.Name())
		if err != nil {
			log.Printf("⚠️  Could not inspect project %s: %v", entry.Name(), err)
			continue
		}
		if project != nil {
			ps.projects = append(ps.projects, *project)
		}
	}

	return nil
}

// inspect determines if a directory is a project and its type.
func (ps *ProjectScanner) inspect(path, name string) (*types.Project, error) {
	// Check for .beads directory at root (single repo)
	beadsPath := filepath.Join(path, ".beads", "beads.db")
	if _, err := os.Stat(beadsPath); err == nil {
		return &types.Project{
			Name: name,
			Path: path,
			Type: types.RepoTypeSingle,
		}, nil
	}

	// Check subdirectories for .beads (multi-repo)
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var repos []types.Repo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subBeadsPath := filepath.Join(path, entry.Name(), ".beads", "beads.db")
		if _, err := os.Stat(subBeadsPath); err == nil {
			repos = append(repos, types.Repo{
				Name: entry.Name(),
				Path: entry.Name(),
			})
		}
	}

	if len(repos) > 0 {
		return &types.Project{
			Name:  name,
			Path:  path,
			Type:  types.RepoTypeMulti,
			Repos: repos,
		}, nil
	}

	return nil, nil
}

// discoverRepos checks subdirectories for .beads/beads.db markers.
func (ps *ProjectScanner) discoverRepos(projectPath string) []types.Repo {
	var repos []types.Repo

	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subBeadsPath := filepath.Join(projectPath, entry.Name(), ".beads", "beads.db")
		if _, err := os.Stat(subBeadsPath); err == nil {
			repos = append(repos, types.Repo{
				Name: entry.Name(),
				Path: entry.Name(),
			})
		}
	}

	return repos
}

// ProjectPath returns the path for a project by name.
func (ps *ProjectScanner) ProjectPath(name string) string {
	for _, project := range ps.projects {
		if project.Name == name {
			return project.Path
		}
	}
	return ""
}
