package core

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// MultiUserDiscovery finds all users with Hydra configs and their projects.
//
// NOTE: This scans /home/<user>/.hydra/config.yaml for per-user Hydra config.
// Pi itself is shared system infrastructure and is NOT discovered per-user.
// Hydra workers inherit Pi configuration from the central Pi installation
// (e.g. /opt/pi with symlinked ~/.pi) via environment variables.
type MultiUserDiscovery struct {
	users    map[string]*UserContext
	projects []types.Project
	blocked  []string // users whose config exists but is unreadable by the daemon
	HomeDir  string   // Base directory to scan (default: /home)
}

// NewMultiUserDiscovery creates a new multi-user discovery instance.
func NewMultiUserDiscovery() *MultiUserDiscovery {
	return &MultiUserDiscovery{
		users:   make(map[string]*UserContext),
		HomeDir: "/home",
	}
}

// Discover scans HomeDir/* for .hydra/config.yaml and loads user configs.
func (d *MultiUserDiscovery) Discover() error {
	base := d.HomeDir
	if base == "" {
		base = "/home"
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return fmt.Errorf("read /home: %w", err)
	}

	// blocked collects users who have a config the daemon cannot read (almost
	// always a home-dir traverse-permission problem) so the skip is surfaced
	// loudly with a remediation, instead of a silent one-line log.
	var blocked []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Skip system users
		name := entry.Name()
		if name == "root" || name == "hydra" || strings.HasPrefix(name, "_") {
			continue
		}

		homeDir := filepath.Join(base, name)
		configPath := filepath.Join(homeDir, ".hydra", "config.yaml")
		if _, err := os.Stat(configPath); err != nil {
			if os.IsNotExist(err) {
				continue // user simply has no hydra config — not a problem
			}
			// A non-not-exist stat error (typically EACCES: the home dir is not
			// traversable by the daemon) means a config may well exist but is
			// unreachable. Surface it — this is the recurring onboarding gap.
			if errors.Is(err, os.ErrPermission) {
				log.Printf("[MultiUserDiscovery] BLOCKED %s: cannot reach %s (%v) — run: hydra-onboard %s", name, configPath, err, name)
				blocked = append(blocked, name)
			} else {
				log.Printf("[MultiUserDiscovery] Skip %s: stat %s: %v", name, configPath, err)
			}
			continue
		}

		ctx, err := LoadUserConfig(homeDir)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				log.Printf("[MultiUserDiscovery] BLOCKED %s: cannot read config (%v) — run: hydra-onboard %s", name, err, name)
				blocked = append(blocked, name)
			} else {
				log.Printf("[MultiUserDiscovery] Skip %s: %v", name, err)
			}
			continue
		}

		d.users[name] = ctx
		log.Printf("[MultiUserDiscovery] Found user: %s (%d projects)", name, len(ctx.Config.Projects))

		for _, up := range ctx.Config.Projects {
			project := d.convertProject(up, ctx)
			if project != nil {
				d.projects = append(d.projects, *project)
			}
		}
	}

	d.blocked = blocked
	if len(blocked) > 0 {
		log.Printf("[MultiUserDiscovery] ⚠️  %d user(s) SKIPPED (config unreadable by daemon): %s — fix with: hydra-onboard <user> (or wait for the onboard sweep)", len(blocked), strings.Join(blocked, ", "))
	}
	log.Printf("[MultiUserDiscovery] Summary: %d users found, %d blocked, %d total projects", len(d.users), len(blocked), len(d.projects))
	return nil
}

// Blocked returns usernames whose config exists but is unreadable by the daemon
// (traverse/permission gap). Exposed for status reporting.
func (d *MultiUserDiscovery) Blocked() []string { return d.blocked }

// Users returns all discovered user contexts.
func (d *MultiUserDiscovery) Users() map[string]*UserContext {
	return d.users
}

// Projects returns all discovered projects (flat list for scheduler).
func (d *MultiUserDiscovery) Projects() []types.Project {
	return d.projects
}

// GetUser returns a user context by username.
func (d *MultiUserDiscovery) GetUser(username string) *UserContext {
	return d.users[username]
}

// convertProject converts a UserProject to a types.Project with auto-discovery.
func (d *MultiUserDiscovery) convertProject(up UserProject, ctx *UserContext) *types.Project {
	// Validate path exists
	if _, err := os.Stat(up.Path); os.IsNotExist(err) {
		log.Printf("[MultiUserDiscovery] Project path not found for %s/%s: %s", ctx.Username, up.Name, up.Path)
		return nil
	}

	project := types.Project{
		Name: up.Name,
		Path: up.Path,
		Type: types.RepoType(up.Type),
	}

	// Auto-discover repos if type is multi and no repos listed
	if up.Type == "multi" || up.Type == "" {
		if len(up.Repos) == 0 || up.AutoDiscover {
			repos := d.discoverRepos(up.Path)
			if len(repos) > 0 {
				project.Type = types.RepoTypeMulti
				project.Repos = repos
			} else if up.Type == "multi" {
				// Explicitly multi but no repos found
				log.Printf("[MultiUserDiscovery] No repos found in multi project %s/%s", ctx.Username, up.Name)
			}
		} else {
			project.Type = types.RepoTypeMulti
			for _, rc := range up.Repos {
				project.Repos = append(project.Repos, types.Repo{
					Name: rc.Name,
					Path: rc.Path,
				})
			}
		}
	} else if up.Type == "single" {
		project.Type = types.RepoTypeSingle
	}

	return &project
}

// discoverRepos checks subdirectories for .beads/beads.db markers.
func (d *MultiUserDiscovery) discoverRepos(projectPath string) []types.Repo {
	var repos []types.Repo

	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		beadsPath := filepath.Join(projectPath, entry.Name(), ".beads", "beads.db")
		if _, err := os.Stat(beadsPath); err == nil {
			repos = append(repos, types.Repo{
				Name: entry.Name(),
				Path: entry.Name(),
			})
		}
	}

	return repos
}
