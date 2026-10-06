package core

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// brConfigRunner abstracts br config get/set so PrefixGuard stays testable
// without requiring a live br binary in unit tests.
type brConfigRunner interface {
	configGet(repoDir, key string) (string, error)
	configSet(repoDir, key, value string) error
}

type realBrConfigRunner struct{}

func (r *realBrConfigRunner) configGet(repoDir, key string) (string, error) {
	cmd := exec.Command("br", "config", "get", key)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("br config get %s in %s: %w", key, repoDir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *realBrConfigRunner) configSet(repoDir, key, value string) error {
	cmd := exec.Command("br", "config", "set", key, value)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("br config set %s=%s in %s: %w", key, value, repoDir, err)
	}
	return nil
}

// PrefixGuard ensures discovered repos use a namespaced br issue_prefix so
// bead IDs do not collide across users or projects that share a repo name.
type PrefixGuard struct {
	runner brConfigRunner
	// rawConfig reads the store's raw .beads/config.yaml so the guard can
	// distinguish br's default prefix (written as a commented line) from an
	// explicitly configured one. Nil falls back to reading the file directly.
	rawConfig func(repoDir string) ([]byte, error)
}

// NewPrefixGuard creates a PrefixGuard that shells out to br.
func NewPrefixGuard() *PrefixGuard {
	return &PrefixGuard{runner: &realBrConfigRunner{}}
}

// GuardProject applies the issue_prefix guard to every repo in a discovered
// project. For single-repo projects the repo name is the project name; for
// multi-repo projects each child repo gets its own prefix.
func (g *PrefixGuard) GuardProject(project types.Project, username string, configured string) {
	if project.Type == types.RepoTypeSingle || project.Type == "" {
		g.guardRepo(project.Path, username, project.Name, configured)
		return
	}
	for _, repo := range project.Repos {
		repoDir := resolveRepoPath(project, repo)
		g.guardRepo(repoDir, username, repo.Name, configured)
	}
}

// guardRepo checks the current issue_prefix and rewrites it when it is unset
// or still at br's default (the repo directory base name). A deliberately
// customized prefix is left alone and logged loudly. Fail-open: br errors are
// logged and ignored so a broken br config never blocks project discovery.
//
// A current prefix equal to the repo basename is only treated as br's default
// when the store's .beads/config.yaml does not explicitly configure it (br
// writes defaults as a commented "# issue_prefix: <name>" line). An explicit
// prefix matching the basename is an established store namespace: renaming it
// mid-life splits the store's ID space (2026-10-05 srv-jobfile: 38
// "srv-jobfile-*" beads vs 11 post-rename "ajantha-srv-jobfile-*" beads,
// including every Pan-filed wedge bead — deps and fix-original references
// across the two prefixes stopped resolving).
func (g *PrefixGuard) guardRepo(repoDir, username, repoName, configured string) {
	desired := issuePrefixForRepo(username, repoName, configured)
	defaultPrefix := strings.ToLower(filepath.Base(repoDir))

	current, err := g.runner.configGet(repoDir, "issue_prefix")
	if err != nil {
		log.Printf("[PrefixGuard] %s: br config get failed (fail-open): %v", repoDir, err)
		return
	}

	if current == "" || current == "br" {
		if err := g.runner.configSet(repoDir, "issue_prefix", desired); err != nil {
			log.Printf("[PrefixGuard] %s: br config set failed (fail-open): %v", repoDir, err)
			return
		}
		log.Printf("[PrefixGuard] Set issue_prefix=%s in %s", desired, repoDir)
		return
	}

	if current == defaultPrefix {
		explicit, err := g.storePrefixExplicit(repoDir)
		if err != nil {
			// Unreadable store config (e.g. the WAL-poisoned store that produced
			// the srv-jobfile rename): preserve the established namespace rather
			// than gamble a mid-life rename on an unreadable state.
			log.Printf("[PrefixGuard] %s: existing issue_prefix=%q (explicit, config unreadable: %v) preserved; desired=%q", repoDir, current, err, desired)
			return
		}
		if explicit {
			log.Printf("[PrefixGuard] %s: existing issue_prefix=%q (explicit in .beads/config.yaml) preserved; desired=%q", repoDir, current, desired)
			return
		}
		if err := g.runner.configSet(repoDir, "issue_prefix", desired); err != nil {
			log.Printf("[PrefixGuard] %s: br config set failed (fail-open): %v", repoDir, err)
			return
		}
		log.Printf("[PrefixGuard] Set issue_prefix=%s in %s (br default was %q)", desired, repoDir, current)
		return
	}

	if current != desired {
		log.Printf("[PrefixGuard] %s: existing issue_prefix=%q (custom) preserved; desired=%q", repoDir, current, desired)
	}
}

// storePrefixExplicit reports whether .beads/config.yaml explicitly configures
// issue_prefix (an uncommented "issue_prefix:" line). A missing file means br
// never wrote a config: the prefix is whatever the runtime resolved, treated
// as default (rename allowed).
func (g *PrefixGuard) storePrefixExplicit(repoDir string) (bool, error) {
	read := g.rawConfig
	if read == nil {
		read = func(dir string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, ".beads", "config.yaml"))
		}
	}
	raw, err := read(repoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("#")) {
			continue
		}
		if bytes.HasPrefix(trimmed, []byte("issue_prefix:")) {
			return true, nil
		}
	}
	return false, nil
}

// LogConfigPrefixCollisionGuidance scans config-listed projects and warns when
// a repo basename is shared with another registered project. Config-listed
// projects are never mutated by PrefixGuard, so operators must add explicit
// issue_prefix values to avoid bead ID collisions.
func LogConfigPrefixCollisionGuidance(configProjects, allProjects []types.Project) {
	seen := make(map[string]string) // repo basename -> project name
	for _, p := range allProjects {
		for _, prefix := range projectDefaultPrefixes(p) {
			if existing, ok := seen[prefix]; ok && existing != p.Name {
				// Collision between two registered projects already noted by the
				// config-project pass below if one of them is config-listed.
				continue
			}
			seen[prefix] = p.Name
		}
	}
	for _, p := range configProjects {
		for _, prefix := range projectDefaultPrefixes(p) {
			if other, ok := seen[prefix]; ok && other != p.Name {
				log.Printf("[PrefixGuard] Config-listed project %q shares repo basename %q with project %q. Add a distinct issue_prefix to the config project to avoid bead ID collisions. Hydra will not mutate config-listed projects.", p.Name, prefix, other)
			}
		}
	}
}

func projectDefaultPrefixes(project types.Project) []string {
	var out []string
	if project.Type == types.RepoTypeSingle || project.Type == "" {
		return append(out, strings.ToLower(filepath.Base(project.Path)))
	}
	for _, r := range project.Repos {
		out = append(out, strings.ToLower(filepath.Base(resolveRepoPath(project, r))))
	}
	return out
}

// An explicitly configured prefix is normalized and used as-is; otherwise
// the prefix is auto-namespaced as <username>-<reponame>.
func issuePrefixForRepo(username, repoName, configured string) string {
	if configured != "" {
		return normalizeIssuePrefix(configured)
	}
	return normalizeIssuePrefix(username + "-" + repoName)
}

// normalizeIssuePrefix lowercases the prefix and collapses runs of
// non-alphanumeric characters to a single hyphen, matching br's own
// normalization for config values.
func normalizeIssuePrefix(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastWasHyphen := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastWasHyphen = false
		default:
			if !lastWasHyphen {
				b.WriteRune('-')
				lastWasHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "beads"
	}
	return out
}
