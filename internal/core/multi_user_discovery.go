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
	users         map[string]*UserContext
	projects      []types.Project
	blocked       []string // users whose config exists but is unreadable by the daemon
	HomeDir       string   // Base directory to scan (default: /home)
	Exclude       []string // project dir names or paths to skip during discovery
	prefixGuard   *PrefixGuard
	projectOwners map[string]string // project key "name|path" -> username
}

// NewMultiUserDiscovery creates a new multi-user discovery instance.
func NewMultiUserDiscovery() *MultiUserDiscovery {
	return &MultiUserDiscovery{
		users:         make(map[string]*UserContext),
		projectOwners: make(map[string]string),
		prefixGuard:   NewPrefixGuard(),
		HomeDir:       "/home",
	}
}

// SetExclude configures project directory names or paths that should be
// skipped during discovery. Applied before any repo discovery or bv triage.
func (d *MultiUserDiscovery) SetExclude(exclude []string) {
	d.Exclude = exclude
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

	// blocked collects users whose config is CONFIRMED present but unreadable
	// (stat succeeds, read fails) — an unambiguous onboarding gap surfaced
	// loudly with a remediation. unreachable counts homes the daemon cannot
	// even traverse: that is indistinguishable from an ordinary non-Hydra user
	// with a locked-down (0700) home and no config, so it is only counted, not
	// blamed on a specific user.
	var blocked []string
	var unreachable int

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
			// EACCES on stat: the home is not traversable by the daemon. This
			// is indistinguishable from an ordinary non-Hydra user with a
			// locked-down home and no config, so count it quietly rather than
			// naming the user with a bogus remediation. The root-run onboard
			// sweep is the authority that can actually traverse and decide.
			if errors.Is(err, os.ErrPermission) {
				unreachable++
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

	d.guardPrefixes()

	d.blocked = blocked
	if len(blocked) > 0 {
		log.Printf("[MultiUserDiscovery] ⚠️  %d user(s) SKIPPED (config present but unreadable by daemon): %s — fix with: hydra-onboard <user> (or wait for the onboard sweep)", len(blocked), strings.Join(blocked, ", "))
	}
	if unreachable > 0 {
		log.Printf("[MultiUserDiscovery] %d home dir(s) not traversable by the daemon (no reachable config; ordinary if they are non-Hydra users). The onboard sweep will pick up any that are Hydra users.", unreachable)
	}
	log.Printf("[MultiUserDiscovery] Summary: %d users found, %d blocked, %d unreachable homes, %d total projects", len(d.users), len(blocked), unreachable, len(d.projects))
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
	// Skip excluded projects before touching the filesystem or invoking bv.
	if d.isExcluded(up.Name) || d.isExcluded(up.Path) {
		log.Printf("[MultiUserDiscovery] Excluding project %s/%s (matched auto_discover_exclude)", ctx.Username, up.Name)
		return nil
	}

	// Validate path exists
	if _, err := os.Stat(up.Path); os.IsNotExist(err) {
		log.Printf("[MultiUserDiscovery] Project path not found for %s/%s: %s", ctx.Username, up.Name, up.Path)
		return nil
	}

	project := types.Project{
		Name:        up.Name,
		Path:        up.Path,
		Type:        types.RepoType(up.Type),
		IssuePrefix: up.IssuePrefix,
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

	// Record ownership so PrefixGuard can attribute repos to users during
	// refresh cycles without baking usernames into the public Project type.
	d.projectOwners[project.Name+"|"+project.Path] = ctx.Username

	return &project
}

// isExcluded reports whether a project name or path matches the exclusion list.
// Entries may be bare directory names or full paths.
func (d *MultiUserDiscovery) isExcluded(name string) bool {
	if name == "" {
		return false
	}
	base := filepath.Base(name)
	for _, e := range d.Exclude {
		if e == name || e == base {
			return true
		}
	}
	return false
}

// discoverRepos checks subdirectories for .beads directory markers.
// Real br projects write .beads/issues.jsonl; older setups may use .beads/beads.db.
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
		if d.isExcluded(entry.Name()) {
			log.Printf("[MultiUserDiscovery] Excluding repo %s (matched auto_discover_exclude)", entry.Name())
			continue
		}
		repoDir := filepath.Join(projectPath, entry.Name())
		if hasBeadsMarker(repoDir) {
			repos = append(repos, types.Repo{
				Name: entry.Name(),
				Path: entry.Name(),
			})
		}
	}

	return repos
}

// hasBeadsMarker reports whether dir contains a br project sentinel file.
func hasBeadsMarker(dir string) bool {
	for _, name := range []string{"issues.jsonl", "beads.db"} {
		if _, err := os.Stat(filepath.Join(dir, ".beads", name)); err == nil {
			return true
		}
	}
	return false
}

// guardPrefixes runs PrefixGuard across all discovered projects. It is called
// once per discovery cycle so newly discovered repos get namespaced on the
// same tick that adds them.
func (d *MultiUserDiscovery) guardPrefixes() {
	for _, p := range d.projects {
		owner, ok := d.projectOwners[d.projectKey(p)]
		if !ok {
			continue
		}
		d.prefixGuard.GuardProject(p, owner, p.IssuePrefix)
	}
}

func (d *MultiUserDiscovery) projectKey(p types.Project) string {
	return p.Name + "|" + p.Path
}