package core

import (
	"path/filepath"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// resolveRepoPath returns the absolute filesystem path for a repo inside a
// project. For multi-repo projects the repo's configured Path is treated as
// relative to the project root unless it is already absolute; an empty Path
// falls back to the repo Name. Single-repo projects simply return project.Path.
//
// This is the single source of truth for deriving a per-repo path from the
// public Project/Repo types. Callers that need a repo path (discovery,
// provenance, spawn, prefix guard, close paths) must use it so relative
// repo.Path values are never passed to git or the worker as if they were
// absolute (srv-telematics incident, 2026-09-21).
func resolveRepoPath(project types.Project, repo types.Repo) string {
	if project.Type != types.RepoTypeMulti {
		return project.Path
	}
	segment := repo.Path
	if segment == "" {
		segment = repo.Name
	}
	if filepath.IsAbs(segment) {
		return segment
	}
	return filepath.Join(project.Path, segment)
}
